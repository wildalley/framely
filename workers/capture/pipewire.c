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
#include <fcntl.h>
#include "frame_protocol.h"
#ifdef SCREENPIN_RECORD
#include "../media/recorder.h"
#endif

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
    unsigned protocol;
    struct sp_region region;
    struct spa_source *input;
    char command[256];
    size_t command_used;
#ifdef SCREENPIN_RECORD
    struct sp_recorder *record;
    struct spa_source *record_timer;
    int recording_started;
    int fps;
    uint64_t status_sent;
#endif
};

#ifdef SCREENPIN_RECORD
static int64_t record_now(void) {struct timespec ts;clock_gettime(CLOCK_MONOTONIC,&ts);return (int64_t)ts.tv_sec*1000000+ts.tv_nsec/1000;}
#endif

static int write_all(const void *data, size_t size) {
    const uint8_t *p=data;
    while(size) { ssize_t n=write(STDOUT_FILENO,p,size); if(n<0&&errno==EINTR)continue; if(n<=0)return -1; p+=n;size-=n; }
    return 0;
}

static void command_received(struct capture *c,const char *line) {
    struct sp_region next={0};
    const char *error="";
#ifdef SCREENPIN_RECORD
    unsigned id=0;char extra;
    int result=0,handled=1;
    if(sscanf(line,"record %u %c",&id,&extra)==1) {
        if(!c->region.generation)result=-1;
        else result=sp_record_begin(c->record,c->region.width,c->region.height,record_now());
        if(!result){
            c->recording_started=1;
            struct timespec disarm={0};pw_loop_update_timer(pw_main_loop_get_loop(c->loop),c->timer,&disarm,NULL,false);
        }
    } else if(sscanf(line,"pause %u %c",&id,&extra)==1)result=sp_record_pause(c->record,1,record_now());
    else if(sscanf(line,"resume %u %c",&id,&extra)==1)result=sp_record_pause(c->record,0,record_now());
    else if(sscanf(line,"finish %u %c",&id,&extra)==1)result=sp_record_finish(c->record,record_now());
    else handled=0;
    if(handled) {
        error=result<0?sp_record_error(c->record):"";
        if(result<0 && !*error)error="Invalid recording state";
        if(!result) {
            char status[512];sp_record_status(c->record,record_now(),status,sizeof(status));
            uint32_t h[]={0x31525053,(uint32_t)strlen(status),0,0};
            if(write_all(h,sizeof(h))<0||write_all(status,strlen(status))<0)pw_main_loop_quit(c->loop);
        }
        uint32_t header[4]={0x31435053,(uint32_t)strlen(error),result<0?1u:0u,id};
        if(write_all(header,sizeof(header))<0||write_all(error,strlen(error))<0)pw_main_loop_quit(c->loop);
        return;
    }
#endif
    if(sp_parse_crop(line,c->format.size.width,c->format.size.height,&next)<0)error="Invalid crop or changed source size";
#ifdef SCREENPIN_RECORD
    else if(c->recording_started || sp_record_select(c->record,next.x,next.y,next.width,next.height,c->format.size.width,c->format.size.height)<0)error="Cannot change recording region";
#endif
    else c->region=next;
    uint32_t header[4]={0x31435053,(uint32_t)strlen(error),*error?1u:0u,next.generation};
    if(write_all(header,sizeof(header))<0||write_all(error,strlen(error))<0)pw_main_loop_quit(c->loop);
}

static void input_ready(void *userdata,int fd,uint32_t mask) {
    struct capture *c=userdata;
    if(mask&SPA_IO_IN) {
        char bytes[256];ssize_t count;
        while((count=read(fd,bytes,sizeof(bytes)))>0) {
            for(ssize_t i=0;i<count;i++) {
                if(bytes[i]=='\n') {
                    c->command[c->command_used]='\0';
                    command_received(c,c->command);c->command_used=0;
                } else if(bytes[i]=='\0'||c->command_used>=sizeof(c->command)-1) {
                    c->failed=1;pw_main_loop_quit(c->loop);return;
                } else c->command[c->command_used++]=bytes[i];
            }
        }
        if(count==0||(count<0&&errno!=EAGAIN&&errno!=EWOULDBLOCK&&errno!=EINTR))pw_main_loop_quit(c->loop);
    }
    if(mask&(SPA_IO_HUP|SPA_IO_ERR))pw_main_loop_quit(c->loop);
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
#ifdef SCREENPIN_RECORD
    fprintf(stderr,"Source frame rate: %u/%u, maximum %u/%u\n",c->format.framerate.num,c->format.framerate.denom,c->format.max_framerate.num,c->format.max_framerate.denom);
#endif
    fprintf(stderr,"PipeWire format: %ux%u format=%u modifier_present=%d modifier=%llu\n",c->format.size.width,c->format.size.height,c->format.format,c->has_modifier,(unsigned long long)c->format.modifier);
    if(c->format.size.width==0||c->format.size.height==0||c->format.size.width>16384||c->format.size.height>16384||(uint64_t)c->format.size.width*c->format.size.height>32000000) {
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
    struct sp_region region=c->region;
    if(region.generation && (region.source_width!=width||region.source_height!=height)) {
        fprintf(stderr,"Capture source dimensions changed after crop\n");c->failed=1;pw_main_loop_quit(c->loop);goto done;
    }
    uint32_t x=region.generation?region.x:0,y0=region.generation?region.y:0;
    uint32_t out_width=region.generation?region.width:width,out_height=region.generation?region.height:height;
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
    pixels+=chunk->offset+(size_t)y0*stride+(size_t)x*4;
#ifdef SCREENPIN_RECORD
    if(c->record) {
        if(sp_record_capture(c->record,pixels,out_width,out_height,stride,pixel_format)<0) {
            fprintf(stderr,"Recording: %s\n",sp_record_error(c->record));c->failed=1;pw_main_loop_quit(c->loop);goto done;
        }
        if(c->recording_started){c->sent=now;goto done;}
    }
#endif
    uint32_t header[9]={c->protocol==2?0x32465053u:0x31465053u,out_width,out_height,pixel_format,x,y0,width,height,region.generation};
    if(write_all(header,c->protocol==2?sizeof(header):16)<0) {pw_main_loop_quit(c->loop);goto done;}
    if(stride==out_width*4) {
        if(write_all(pixels,(size_t)out_width*out_height*4)<0) {pw_main_loop_quit(c->loop);goto done;}
    } else {
        for(uint32_t y=0;y<out_height;y++) if(write_all(pixels+(size_t)y*stride,out_width*4)<0) {pw_main_loop_quit(c->loop);goto done;}
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
    struct pw_buffer *pw=NULL,*next;
    while((next=pw_stream_dequeue_buffer(c->stream))!=NULL){if(pw)pw_stream_queue_buffer(c->stream,pw);pw=next;}
    if(!pw)return;
    if(c->pending){pw_stream_queue_buffer(c->stream,c->pending);c->pending=NULL;}
#ifdef SCREENPIN_RECORD
    // Own a bounded latest-frame copy and promptly return compositor buffers.
    if(c->recording_started){emit_buffer(c,pw);return;}
#endif
    struct timespec ts;clock_gettime(CLOCK_MONOTONIC,&ts);
    uint64_t now=(uint64_t)ts.tv_sec*1000000000+ts.tv_nsec;
    uint64_t interval=40000000;
#ifdef SCREENPIN_RECORD
    if(c->recording_started)interval=0;
#endif
    if(c->sent&&now-c->sent<interval) {
        // Damage-only streams still need their final settled frame flushed.
        uint64_t delay=interval-(now-c->sent);
        struct timespec next={.tv_sec=0,.tv_nsec=(long)delay};
        c->pending=pw;
        pw_loop_update_timer(pw_main_loop_get_loop(c->loop),c->timer,&next,NULL,false);
        return;
    }
    emit_buffer(c,pw);
}

#ifdef SCREENPIN_RECORD
static void record_tick(void *userdata,uint64_t expirations) {
    (void)expirations;struct capture *c=userdata;
    // Timestamp the sampling instant, before conversion. Alternating fresh
    // and static frames otherwise introduce half-frame timestamp jitter.
    int64_t now=record_now();
    if(c->recording_started)flush_pending(c,0);
    if(sp_record_tick(c->record,now)<0) {
        fprintf(stderr,"Recording: %s\n",sp_record_error(c->record));c->failed=1;pw_main_loop_quit(c->loop);return;
    }
    if(c->recording_started && now-(int64_t)c->status_sent>=250000) {
        char status[512];sp_record_status(c->record,now,status,sizeof(status));c->status_sent=now;
        uint32_t header[4]={0x31525053,(uint32_t)strlen(status),0,0};
        if(write_all(header,sizeof(header))<0||write_all(status,strlen(status))<0)pw_main_loop_quit(c->loop);
    }
}
#endif

static const struct pw_stream_events events={PW_VERSION_STREAM_EVENTS,.state_changed=state_changed,.param_changed=param_changed,.remove_buffer=remove_buffer,.process=process};

static void capture_formats(struct spa_pod_builder *builder,const struct spa_pod **params,unsigned fps) {
    struct spa_pod_frame frame;
    spa_pod_builder_push_object(builder,&frame,SPA_TYPE_OBJECT_Format,SPA_PARAM_EnumFormat);
    spa_pod_builder_add(builder,
        SPA_FORMAT_mediaType,SPA_POD_Id(SPA_MEDIA_TYPE_video),SPA_FORMAT_mediaSubtype,SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
        SPA_FORMAT_VIDEO_format,SPA_POD_CHOICE_ENUM_Id(5,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRx,SPA_VIDEO_FORMAT_BGRA,SPA_VIDEO_FORMAT_RGBA,SPA_VIDEO_FORMAT_RGBx),
        SPA_FORMAT_VIDEO_size,SPA_POD_CHOICE_RANGE_Rectangle(&SPA_RECTANGLE(1920,1080),&SPA_RECTANGLE(1,1),&SPA_RECTANGLE(16384,16384)),
        SPA_FORMAT_VIDEO_framerate,SPA_POD_CHOICE_RANGE_Fraction(&SPA_FRACTION(fps,1),&SPA_FRACTION(0,1),&SPA_FRACTION(144,1)),0);
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
        SPA_FORMAT_VIDEO_framerate,SPA_POD_CHOICE_RANGE_Fraction(&SPA_FRACTION(fps,1),&SPA_FRACTION(0,1),&SPA_FRACTION(144,1)));
}

int main(int argc,char **argv) {
    if(argc==2&&!strcmp(argv[1],"--protocol-version")){puts("2");return 0;}
    int fd=-1;uint32_t node=PW_ID_ANY;unsigned protocol=1;
#ifdef SCREENPIN_RECORD
    struct sp_record_options recording={NULL,60,1920,"auto"};
#endif
    for(int i=1;i+1<argc;i+=2) {if(!strcmp(argv[i],"--fd"))fd=atoi(argv[i+1]);else if(!strcmp(argv[i],"--node"))node=(uint32_t)strtoul(argv[i+1],NULL,10);else if(!strcmp(argv[i],"--protocol"))protocol=(unsigned)atoi(argv[i+1]);
#ifdef SCREENPIN_RECORD
        else if(!strcmp(argv[i],"--record"))recording.path=argv[i+1];
        else if(!strcmp(argv[i],"--fps"))recording.fps=atoi(argv[i+1]);
        else if(!strcmp(argv[i],"--max-edge"))recording.max_edge=atoi(argv[i+1]);
        else if(!strcmp(argv[i],"--codec"))recording.codec=argv[i+1];
#endif
        else return 2;}
    if(protocol!=1&&protocol!=2)return 2;
    if(fd<0||node==PW_ID_ANY) {fprintf(stderr,"Expected an authorized PipeWire fd and node\n");return 2;}
    signal(SIGPIPE,SIG_IGN);pw_init(NULL,NULL);
    struct capture c={0};c.protocol=protocol;c.loop=pw_main_loop_new(NULL);
#ifdef SCREENPIN_RECORD
    if(!c.loop)return 1;
    c.record=sp_record_new(recording);c.fps=recording.fps;if(!c.record)return 2;
    c.record_timer=pw_loop_add_timer(pw_main_loop_get_loop(c.loop),record_tick,&c);
    if(!c.record_timer)return 1;
    struct timespec tick={.tv_sec=0,.tv_nsec=1000000000/recording.fps};
    pw_loop_update_timer(pw_main_loop_get_loop(c.loop),c.record_timer,&tick,&tick,false);
#endif
    if(!c.loop)return 1;
    c.timer=pw_loop_add_timer(pw_main_loop_get_loop(c.loop),flush_pending,&c);
    if(!c.timer)return 1;
    if(protocol==2) {
        int flags=fcntl(STDIN_FILENO,F_GETFL);
        if(flags<0||fcntl(STDIN_FILENO,F_SETFL,flags|O_NONBLOCK)<0)return 1;
        c.input=pw_loop_add_io(pw_main_loop_get_loop(c.loop),STDIN_FILENO,SPA_IO_IN|SPA_IO_HUP|SPA_IO_ERR,false,input_ready,&c);
        if(!c.input)return 1;
    }
    struct pw_context *context=pw_context_new(pw_main_loop_get_loop(c.loop),NULL,0);
    struct pw_core *core=context?pw_context_connect_fd(context,fd,NULL,0):NULL;
    if(!core){fprintf(stderr,"Cannot connect to authorized PipeWire remote\n");return 1;}
    struct spa_hook core_listener;pw_core_add_listener(core,&core_listener,&core_events,&c);
    fprintf(stderr,"PipeWire authorized source node: %u\n",node);
    struct pw_properties *props=pw_properties_new(PW_KEY_MEDIA_TYPE,"Video",PW_KEY_MEDIA_CATEGORY,"Capture",PW_KEY_MEDIA_ROLE,"Screen",NULL);
    c.stream=pw_stream_new(core,"Framely screen capture",props);
    if(!c.stream)return 1;
    struct spa_hook listener;pw_stream_add_listener(c.stream,&listener,&events,&c);
    uint8_t storage[1024];struct spa_pod_builder builder=SPA_POD_BUILDER_INIT(storage,sizeof(storage));
    const struct spa_pod *params[2];
#ifdef SCREENPIN_RECORD
    capture_formats(&builder,params,(unsigned)c.fps);
#else
    capture_formats(&builder,params,0);
#endif
    // The portal returns a node ID, not an object.serial for target.object.
    if(pw_stream_connect(c.stream,PW_DIRECTION_INPUT,node,PW_STREAM_FLAG_AUTOCONNECT|PW_STREAM_FLAG_MAP_BUFFERS,params,2)<0){fprintf(stderr,"Cannot start PipeWire stream\n");return 1;}
    pw_main_loop_run(c.loop);
#ifdef SCREENPIN_RECORD
    pw_loop_destroy_source(pw_main_loop_get_loop(c.loop),c.record_timer);
    if(sp_record_active(c.record) && sp_record_finish(c.record,record_now())<0)fprintf(stderr,"Finalize interrupted recording: %s\n",sp_record_error(c.record));
    sp_record_free(c.record);
#endif
    if(c.input)pw_loop_destroy_source(pw_main_loop_get_loop(c.loop),c.input);
    pw_loop_destroy_source(pw_main_loop_get_loop(c.loop),c.timer);
    pw_stream_destroy(c.stream);pw_core_disconnect(core);pw_context_destroy(context);pw_main_loop_destroy(c.loop);pw_deinit();
    return c.failed?1:0;
}
