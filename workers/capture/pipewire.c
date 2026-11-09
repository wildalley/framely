// ScreenPin's CPU-frame PipeWire bridge. No window system or input bypass.
#define _POSIX_C_SOURCE 200809L
#include <pipewire/pipewire.h>
#include <spa/param/video/format-utils.h>
#include <spa/param/param.h>
#include <spa/pod/builder.h>
#include <errno.h>
#include <signal.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>
#include <sys/mman.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <linux/dma-buf.h>

struct capture {
    struct pw_main_loop *loop;
    struct pw_stream *stream;
    struct spa_video_info_raw format;
    bool has_modifier;
    struct spa_source *timer;
    struct pw_buffer *pending;
    uint64_t sent;
    int failed;
    unsigned received;
};

static int write_all(const void *data, size_t size) {
    const uint8_t *p=data;
    while(size) { ssize_t n=write(STDOUT_FILENO,p,size); if(n<0&&errno==EINTR)continue; if(n<=0)return -1; p+=n;size-=n; }
    return 0;
}

static void state_changed(void *userdata, enum pw_stream_state old, enum pw_stream_state state, const char *error) {
    struct capture *c=userdata;
    fprintf(stderr,"PipeWire state: %s -> %s%s%s\n",pw_stream_state_as_string(old),pw_stream_state_as_string(state),error?": ":"",error?error:"");
    if(state==PW_STREAM_STATE_ERROR) { fprintf(stderr,"PipeWire: %s\n",error?error:"stream failed");c->failed=1;pw_main_loop_quit(c->loop); }
    if(state==PW_STREAM_STATE_UNCONNECTED && c->sent) pw_main_loop_quit(c->loop);
}

static void core_error(void *userdata,uint32_t id,int seq,int res,const char *message) {
    (void)seq;struct capture *c=userdata;
    fprintf(stderr,"PipeWire core error: id=%u code=%d %s\n",id,res,message?message:"");
    c->failed=1;pw_main_loop_quit(c->loop);
}

static const struct pw_core_events core_events={PW_VERSION_CORE_EVENTS,.error=core_error};

static void param_changed(void *userdata,uint32_t id,const struct spa_pod *param) {
    struct capture *c=userdata;
    if(id!=SPA_PARAM_Format||param==NULL)return;
    if(spa_format_video_raw_parse(param,&c->format)<0)return;
    // Older SPA headers expose modifier presence through the format pod only.
    c->has_modifier=spa_pod_find_prop(param,NULL,SPA_FORMAT_VIDEO_modifier)!=NULL;
    fprintf(stderr,"PipeWire format: %ux%u format=%u modifier_present=%d modifier=%llu\n",c->format.size.width,c->format.size.height,c->format.format,c->has_modifier,(unsigned long long)c->format.modifier);
    if(c->format.size.width==0||c->format.size.height==0||(uint64_t)c->format.size.width*c->format.size.height>32000000) {
        fprintf(stderr,"PipeWire image exceeds capture limits\n");c->failed=1;pw_main_loop_quit(c->loop);return;
    }
    if(c->has_modifier&&c->format.modifier!=0) {
        fprintf(stderr,"Only linear DMA-BUF pixels can be read by this capture component\n");c->failed=1;pw_main_loop_quit(c->loop);return;
    }
    uint8_t storage[1024];struct spa_pod_builder builder=SPA_POD_BUILDER_INIT(storage,sizeof(storage));
    const struct spa_pod *params[1];
    params[0]=spa_pod_builder_add_object(&builder,SPA_TYPE_OBJECT_ParamBuffers,SPA_PARAM_Buffers,
        SPA_PARAM_BUFFERS_buffers,SPA_POD_CHOICE_RANGE_Int(3,2,8),
        SPA_PARAM_BUFFERS_blocks,SPA_POD_Int(1),
        SPA_PARAM_BUFFERS_size,SPA_POD_Int(c->format.size.width*c->format.size.height*4),
        SPA_PARAM_BUFFERS_stride,SPA_POD_Int(c->format.size.width*4),
        SPA_PARAM_BUFFERS_dataType,SPA_POD_CHOICE_FLAGS_Int(c->has_modifier?(1<<SPA_DATA_DmaBuf):((1<<SPA_DATA_MemFd)|(1<<SPA_DATA_MemPtr))));
    pw_stream_update_params(c->stream,params,1);
}

static void emit_buffer(struct capture *c,struct pw_buffer *pw) {
    struct spa_buffer *b=pw->buffer;
    struct timespec ts;clock_gettime(CLOCK_MONOTONIC,&ts);
    uint64_t now=(uint64_t)ts.tv_sec*1000000000+ts.tv_nsec;
    void *mapping=MAP_FAILED;size_t map_size=0;int dma_fd=-1;
    if(!b->n_datas||!b->datas[0].chunk)goto done;
    struct spa_data *data=&b->datas[0];struct spa_chunk *chunk=data->chunk;
    if(c->received++<2)fprintf(stderr,"PipeWire buffer: type=%u maxsize=%u mapoffset=%u offset=%u size=%u stride=%d mapped=%d\n",data->type,data->maxsize,data->mapoffset,chunk->offset,chunk->size,chunk->stride,data->data!=NULL);
    uint32_t width=c->format.size.width,height=c->format.size.height;
    if(!width||!height)goto done;
    uint32_t stride=chunk->stride>0?(uint32_t)chunk->stride:width*4;
    uint64_t bytes=(uint64_t)(height-1)*stride+width*4;
    if(stride<width*4)goto done;
    const uint8_t *pixels=data->data;
    if(data->type==SPA_DATA_DmaBuf) {
        if(!c->has_modifier||c->format.modifier!=0||data->fd<0)goto done;
        dma_fd=(int)data->fd;
        struct dma_buf_sync sync={.flags=DMA_BUF_SYNC_START|DMA_BUF_SYNC_READ};
        if(ioctl(dma_fd,DMA_BUF_IOCTL_SYNC,&sync)<0){fprintf(stderr,"Cannot synchronize linear DMA-BUF: %s\n",strerror(errno));c->failed=1;pw_main_loop_quit(c->loop);goto done;}
        {
            // DMA-BUF producers may report maxsize/chunk.size=1 as a sentinel.
            // Linear packed pixels are bounded by the stride, height and fd.
            uint64_t extent=(uint64_t)data->mapoffset+chunk->offset+bytes;
            struct stat info;
            if(extent>512u*1024u*1024u||(fstat(dma_fd,&info)==0&&info.st_size>0&&extent>(uint64_t)info.st_size)) {
                fprintf(stderr,"Linear DMA-BUF dimensions exceed backing storage\n");c->failed=1;pw_main_loop_quit(c->loop);goto done;
            }
            map_size=(size_t)extent;
            mapping=mmap(NULL,map_size,PROT_READ,MAP_SHARED,dma_fd,0);
            if(mapping==MAP_FAILED){fprintf(stderr,"Cannot map linear DMA-BUF: %s\n",strerror(errno));c->failed=1;pw_main_loop_quit(c->loop);goto done;}
            pixels=(const uint8_t*)mapping+data->mapoffset;
        }
    } else if(chunk->offset>data->maxsize||bytes>data->maxsize-chunk->offset) {
        goto done;
    }
    if(!pixels)goto done;
    uint32_t pixel_format;
    switch(c->format.format) {
        case SPA_VIDEO_FORMAT_BGRA:pixel_format=1;break;
        case SPA_VIDEO_FORMAT_RGBA:pixel_format=2;break;
        case SPA_VIDEO_FORMAT_BGRx:pixel_format=3;break;
        case SPA_VIDEO_FORMAT_RGBx:pixel_format=4;break;
        default:goto done;
    }
    uint32_t header[4]={0x31465053,width,height,pixel_format};
    if(write_all(header,sizeof(header))<0) {pw_main_loop_quit(c->loop);goto done;}
    pixels+=chunk->offset;
    if(stride==width*4) {
        if(write_all(pixels,(size_t)width*height*4)<0) {pw_main_loop_quit(c->loop);goto done;}
    } else {
        for(uint32_t y=0;y<height;y++) if(write_all(pixels+y*stride,width*4)<0) {pw_main_loop_quit(c->loop);goto done;}
    }
    c->sent=now;
done:
    if(mapping!=MAP_FAILED)munmap(mapping,map_size);
    if(dma_fd>=0){struct dma_buf_sync sync={.flags=DMA_BUF_SYNC_END|DMA_BUF_SYNC_READ};(void)ioctl(dma_fd,DMA_BUF_IOCTL_SYNC,&sync);}
    pw_stream_queue_buffer(c->stream,pw);
}

static void flush_pending(void *userdata,uint64_t expirations) {
    (void)expirations;struct capture *c=userdata;
    struct pw_buffer *pw=c->pending;c->pending=NULL;
    if(pw)emit_buffer(c,pw);
}

static void remove_buffer(void *userdata,struct pw_buffer *buffer) {
    struct capture *c=userdata;
    if(c->pending==buffer)c->pending=NULL;
}

static void process(void *userdata) {
    struct capture *c=userdata;
    struct pw_buffer *pw=pw_stream_dequeue_buffer(c->stream);
    if(!pw)return;
    if(c->pending){pw_stream_queue_buffer(c->stream,c->pending);c->pending=NULL;}
    struct timespec ts;clock_gettime(CLOCK_MONOTONIC,&ts);
    uint64_t now=(uint64_t)ts.tv_sec*1000000000+ts.tv_nsec;
    if(c->sent&&now-c->sent<40000000) {
        // Damage-only streams still need their final settled frame flushed.
        uint64_t delay=40000000-(now-c->sent);
        struct timespec next={.tv_sec=0,.tv_nsec=(long)delay};
        c->pending=pw;
        pw_loop_update_timer(pw_main_loop_get_loop(c->loop),c->timer,&next,NULL,false);
        return;
    }
    emit_buffer(c,pw);
}

static const struct pw_stream_events events={PW_VERSION_STREAM_EVENTS,.state_changed=state_changed,.param_changed=param_changed,.remove_buffer=remove_buffer,.process=process};

static void capture_formats(struct spa_pod_builder *builder,const struct spa_pod **params) {
    struct spa_pod_frame frame;
    spa_pod_builder_push_object(builder,&frame,SPA_TYPE_OBJECT_Format,SPA_PARAM_EnumFormat);
    spa_pod_builder_add(builder,
        SPA_FORMAT_mediaType,SPA_POD_Id(SPA_MEDIA_TYPE_video),SPA_FORMAT_mediaSubtype,SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
        SPA_FORMAT_VIDEO_format,SPA_POD_CHOICE_ENUM_Id(5,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRA,SPA_VIDEO_FORMAT_RGBA,SPA_VIDEO_FORMAT_RGBx),
        SPA_FORMAT_VIDEO_size,SPA_POD_CHOICE_RANGE_Rectangle(&SPA_RECTANGLE(1920,1080),&SPA_RECTANGLE(1,1),&SPA_RECTANGLE(16384,16384)),
        SPA_FORMAT_VIDEO_framerate,SPA_POD_CHOICE_RANGE_Fraction(&SPA_FRACTION(0,1),&SPA_FRACTION(0,1),&SPA_FRACTION(144,1)),0);
    // Without MANDATORY, SPA can merge our modifier into a producer's CPU
    // format. Mutter then receives a DMA-BUF-only layout for its MemFd source
    // and produces unallocated buffers (invalid memory type 8).
    spa_pod_builder_prop(builder,SPA_FORMAT_VIDEO_modifier,SPA_POD_PROP_FLAG_MANDATORY);
    spa_pod_builder_long(builder,0);
    params[0]=spa_pod_builder_pop(builder,&frame);
    params[1]=spa_pod_builder_add_object(builder,SPA_TYPE_OBJECT_Format,SPA_PARAM_EnumFormat,
        SPA_FORMAT_mediaType,SPA_POD_Id(SPA_MEDIA_TYPE_video),SPA_FORMAT_mediaSubtype,SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
        SPA_FORMAT_VIDEO_format,SPA_POD_CHOICE_ENUM_Id(5,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRA,SPA_VIDEO_FORMAT_RGBA,SPA_VIDEO_FORMAT_RGBx),
        SPA_FORMAT_VIDEO_size,SPA_POD_CHOICE_RANGE_Rectangle(&SPA_RECTANGLE(1920,1080),&SPA_RECTANGLE(1,1),&SPA_RECTANGLE(16384,16384)),
        SPA_FORMAT_VIDEO_framerate,SPA_POD_CHOICE_RANGE_Fraction(&SPA_FRACTION(0,1),&SPA_FRACTION(0,1),&SPA_FRACTION(144,1)));
}

int main(int argc,char **argv) {
    int fd=-1;uint32_t node=PW_ID_ANY;
    for(int i=1;i+1<argc;i+=2) {if(!strcmp(argv[i],"--fd"))fd=atoi(argv[i+1]);else if(!strcmp(argv[i],"--node"))node=(uint32_t)strtoul(argv[i+1],NULL,10);else return 2;}
    if(fd<0||node==PW_ID_ANY) {fprintf(stderr,"Expected an authorized PipeWire fd and node\n");return 2;}
    signal(SIGPIPE,SIG_IGN);pw_init(NULL,NULL);
    struct capture c={0};c.loop=pw_main_loop_new(NULL);
    if(!c.loop)return 1;
    c.timer=pw_loop_add_timer(pw_main_loop_get_loop(c.loop),flush_pending,&c);
    if(!c.timer)return 1;
    struct pw_context *context=pw_context_new(pw_main_loop_get_loop(c.loop),NULL,0);
    struct pw_core *core=context?pw_context_connect_fd(context,fd,NULL,0):NULL;
    if(!core){fprintf(stderr,"Cannot connect to authorized PipeWire remote\n");return 1;}
    struct spa_hook core_listener;pw_core_add_listener(core,&core_listener,&core_events,&c);
    fprintf(stderr,"PipeWire authorized source node: %u\n",node);
    struct pw_properties *props=pw_properties_new(PW_KEY_MEDIA_TYPE,"Video",PW_KEY_MEDIA_CATEGORY,"Capture",PW_KEY_MEDIA_ROLE,"Screen",NULL);
    c.stream=pw_stream_new(core,"ScreenPin long screenshot",props);
    if(!c.stream)return 1;
    struct spa_hook listener;pw_stream_add_listener(c.stream,&listener,&events,&c);
    uint8_t storage[1024];struct spa_pod_builder builder=SPA_POD_BUILDER_INIT(storage,sizeof(storage));
    const struct spa_pod *params[2];
    capture_formats(&builder,params);
    // The portal returns a node ID, not an object.serial for target.object.
    if(pw_stream_connect(c.stream,PW_DIRECTION_INPUT,node,PW_STREAM_FLAG_AUTOCONNECT|PW_STREAM_FLAG_MAP_BUFFERS,params,2)<0){fprintf(stderr,"Cannot start PipeWire stream\n");return 1;}
    pw_main_loop_run(c.loop);
    pw_loop_destroy_source(pw_main_loop_get_loop(c.loop),c.timer);
    pw_stream_destroy(c.stream);pw_core_disconnect(core);pw_context_destroy(context);pw_main_loop_destroy(c.loop);pw_deinit();
    return c.failed?1:0;
}
