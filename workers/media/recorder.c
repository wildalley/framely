// Bounded native encoder. Pixels never cross the application IPC while recording.
#include "recorder.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <libavcodec/avcodec.h>
#include <libavformat/avformat.h>
#include <libavutil/hwcontext.h>
#include <libavutil/opt.h>
#include <libavutil/time.h>
#include <libswscale/swscale.h>

struct sp_recorder {
    struct sp_record_options options;
    AVFormatContext *output;
    AVCodecContext *codec;
    AVStream *stream;
    AVFrame *frame, *hardware;
    AVPacket *packet;
    struct SwsContext *scale;
    int source_width, source_height, active, paused, ready, finished, failed;
    int64_t start, pause_start, paused_us, last_pts, frames, missed, captured, encoded_capture, repeated, convert_us, encode_us;
    char error[256];
    const char *encoder;
    uint8_t *preview;
    int preview_width, preview_height, preview_format, pending;
};
static int fail(struct sp_recorder *r, int code, const char *where) {
    char message[128];av_strerror(code,message,sizeof(message));
    snprintf(r->error,sizeof(r->error),"%s: %s",where,message);r->failed=1;return -1;
}
struct sp_recorder *sp_record_new(struct sp_record_options options) {
    if(!options.path || (options.fps!=30 && options.fps!=60) || options.max_edge<320 || options.max_edge>3840)return NULL;
    struct sp_recorder *r=calloc(1,sizeof(*r));if(!r)return NULL;
    r->options=options;r->last_pts=-1;return r;
}
static void release_codec(struct sp_recorder *r) {
    avcodec_free_context(&r->codec);av_frame_free(&r->hardware);
}
static int open_codec(struct sp_recorder *r,const char *name,int width,int height) {
    const AVCodec *encoder=avcodec_find_encoder_by_name(name);if(!encoder)return -1;
    AVCodecContext *c=avcodec_alloc_context3(encoder);if(!c)return -1;
    r->codec=c;c->width=width;c->height=height;c->time_base=(AVRational){1,r->options.fps};
    c->framerate=(AVRational){r->options.fps,1};c->gop_size=r->options.fps*2;c->max_b_frames=0;
    c->thread_count=4;c->bit_rate=(int64_t)width*height*r->options.fps/8;
    if(c->bit_rate<2000000)c->bit_rate=2000000;
    c->pix_fmt=AV_PIX_FMT_YUV420P;c->flags|=AV_CODEC_FLAG_GLOBAL_HEADER;
    // NVENC accepts packed BGRA and performs RGB conversion on the GPU.
    if(!strcmp(name,"h264_nvenc"))c->pix_fmt=AV_PIX_FMT_BGRA;
    if(!strcmp(name,"h264_vaapi")) {
        AVBufferRef *device=NULL;
        if(av_hwdevice_ctx_create(&device,AV_HWDEVICE_TYPE_VAAPI,NULL,NULL,0)<0)goto bad;
        c->hw_frames_ctx=av_hwframe_ctx_alloc(device);av_buffer_unref(&device);
        if(!c->hw_frames_ctx)goto bad;
        AVHWFramesContext *frames=(AVHWFramesContext*)c->hw_frames_ctx->data;
        frames->format=AV_PIX_FMT_VAAPI;frames->sw_format=AV_PIX_FMT_NV12;frames->width=width;frames->height=height;frames->initial_pool_size=4;
        if(av_hwframe_ctx_init(c->hw_frames_ctx)<0)goto bad;
        c->pix_fmt=AV_PIX_FMT_VAAPI;
        r->hardware=av_frame_alloc();if(!r->hardware || av_hwframe_get_buffer(c->hw_frames_ctx,r->hardware,0)<0)goto bad;
    } else if(!strcmp(name,"h264_qsv") || !strcmp(name,"h264_mf") || !strcmp(name,"h264_amf"))c->pix_fmt=AV_PIX_FMT_NV12;
    AVDictionary *options=NULL;
    if(!strcmp(name,"h264_nvenc")){av_dict_set(&options,"preset","p1",0);av_dict_set(&options,"tune","ull",0);}
    if(!strcmp(name,"libx264")){av_dict_set(&options,"preset","veryfast",0);av_dict_set(&options,"tune","zerolatency",0);}
    if(!strcmp(name,"h264_videotoolbox"))av_dict_set(&options,"allow_sw","0",0);
    if(!strcmp(name,"h264_mf"))av_dict_set(&options,"hw_encoding","1",0);
    int result=avcodec_open2(c,encoder,&options);av_dict_free(&options);
    if(result<0)goto bad;
    r->encoder=name;return 0;
bad:
    release_codec(r);return -1;
}
// A static desktop may never emit another frame after selection. Crop the
// bounded preview cache immediately so recording can still start correctly.
int sp_record_select(struct sp_recorder *r,int x,int y,int width,int height,int source_width,int source_height) {
    if(!r || r->active || r->finished || x<0 || y<0 || width<2 || height<2 ||
       (int64_t)x+width>source_width || (int64_t)y+height>source_height ||
       !r->preview || r->preview_width!=source_width || r->preview_height!=source_height)
        return -1;
    uint8_t *crop=av_malloc((size_t)width*height*4);if(!crop)return fail(r,AVERROR(ENOMEM),"Crop preview");
    for(int row=0;row<height;row++)memcpy(crop+(size_t)row*width*4,r->preview+((size_t)(y+row)*source_width+x)*4,(size_t)width*4);
    av_freep(&r->preview);r->preview=crop;r->preview_width=width;r->preview_height=height;
    return 0;
}
int sp_record_begin(struct sp_recorder *r,int width,int height,int64_t now) {
    if(!r || r->active || r->finished || r->failed || width<2 || height<2 || width>16384 || height>16384)return -1;
    r->source_width=width;r->source_height=height;
    double factor=(double)r->options.max_edge/(width>height?width:height);if(factor>1)factor=1;
    int w=((int)(width*factor))&~1,h=((int)(height*factor))&~1;if(w<2)w=2;if(h<2)h=2;
    const char *names[]={"h264_nvenc","h264_vaapi","h264_qsv","h264_amf","h264_mf","h264_videotoolbox","libx264","mpeg4",NULL};
    if(r->options.codec && strcmp(r->options.codec,"auto")) {
        if(open_codec(r,r->options.codec,w,h)<0)return fail(r,AVERROR(ENOSYS),"Selected encoder unavailable");
    } else {
        for(int i=0;names[i];i++)if(open_codec(r,names[i],w,h)==0)break;
        if(!r->codec)return fail(r,AVERROR(ENOSYS),"No usable video encoder");
    }
    int result=avformat_alloc_output_context2(&r->output,NULL,"mp4",r->options.path);
    if(result<0)return fail(r,result,"Create MP4");
    r->stream=avformat_new_stream(r->output,NULL);if(!r->stream)return fail(r,AVERROR(ENOMEM),"Create stream");
    r->stream->time_base=r->codec->time_base;r->stream->avg_frame_rate=r->codec->framerate;
    if((result=avcodec_parameters_from_context(r->stream->codecpar,r->codec))<0)return fail(r,result,"Codec parameters");
    if((result=avio_open(&r->output->pb,r->options.path,AVIO_FLAG_WRITE))<0)return fail(r,result,"Open video file");
    AVDictionary *opts=NULL;
    // Finished fragments remain recoverable if power is lost. Go publishes only a finalized file.
    av_dict_set(&opts,"movflags","frag_keyframe+empty_moov+default_base_moof",0);
    result=avformat_write_header(r->output,&opts);av_dict_free(&opts);
    if(result<0)return fail(r,result,"Write MP4 header");
    r->frame=av_frame_alloc();r->packet=av_packet_alloc();if(!r->frame||!r->packet)return fail(r,AVERROR(ENOMEM),"Allocate frame");
    r->frame->width=w;r->frame->height=h;r->frame->format=r->hardware?AV_PIX_FMT_NV12:r->codec->pix_fmt;
    if((result=av_frame_get_buffer(r->frame,32))<0)return fail(r,result,"Allocate pixels");
    r->start=now;r->active=1;
    if(r->preview && r->preview_width==width && r->preview_height==height) {
        result=sp_record_frame(r,r->preview,width,height,width*4,r->preview_format);
        av_freep(&r->preview);if(result<0)return result;
    }
    return 0;
}
int sp_record_frame(struct sp_recorder *r,const uint8_t *pixels,int width,int height,int stride,int format) {
    if(!r || !pixels || format<1 || format>4)return -1;
    if(r->finished || r->paused)return 0;
    if(r->failed)return -1;
    if(!r->active) {
        if(width<2||height<2||(int64_t)width*height>32000000||stride<width*4)return -1;
        if(r->preview_width!=width||r->preview_height!=height) {
            av_freep(&r->preview);r->preview=av_malloc((size_t)width*height*4);
            if(!r->preview)return fail(r,AVERROR(ENOMEM),"Prepare selection frame");
        }
        r->preview_width=width;r->preview_height=height;r->preview_format=format;
        for(int y=0;y<height;y++)memcpy(r->preview+(size_t)y*width*4,pixels+(size_t)y*stride,(size_t)width*4);
        return 0;
    }
    if(width!=r->source_width||height!=r->source_height||stride<width*4||format<1||format>4)return fail(r,AVERROR(EINVAL),"Capture dimensions changed");
    int64_t started=av_gettime_relative();
    enum AVPixelFormat input=(format==1||format==3)?AV_PIX_FMT_BGRA:AV_PIX_FMT_RGBA;
    int result=av_frame_make_writable(r->frame);if(result<0)return fail(r,result,"Prepare pixels");
    if(width==r->frame->width && height==r->frame->height && input==r->frame->format) {
        for(int y=0;y<height;y++)memcpy(r->frame->data[0]+(size_t)y*r->frame->linesize[0],pixels+(size_t)y*stride,(size_t)width*4);
    } else {
        if(!r->scale) {
            r->scale=sws_alloc_context();if(!r->scale)return fail(r,AVERROR(ENOMEM),"Create pixel converter");
            av_opt_set_int(r->scale,"threads",4,0);av_opt_set_int(r->scale,"sws_flags",SWS_FAST_BILINEAR,0);
        }
        AVFrame src={0};src.width=width;src.height=height;src.format=input;
        src.colorspace=AVCOL_SPC_RGB;src.color_range=AVCOL_RANGE_JPEG;src.color_primaries=AVCOL_PRI_UNSPECIFIED;src.color_trc=AVCOL_TRC_UNSPECIFIED;
        src.sample_aspect_ratio=(AVRational){1,1};
        src.data[0]=(uint8_t*)pixels;src.linesize[0]=stride;
        // Dynamic frame API runs the scaler's bounded worker pool; legacy
        // sws_scale ignores its slice threads and blocked desktop capture.
        result=sws_scale_frame(r->scale,r->frame,&src);
        if(result<0)return fail(r,result,"Convert pixels");
    }
    r->convert_us+=av_gettime_relative()-started;
    r->ready=1;r->captured++;return 0;
}
// Release the OS buffer promptly. Holding a compositor buffer until the next
// encoding tick halves capture throughput on some two-buffer PipeWire sources.
// A single owned raw frame is replaced in place; no unbounded frame queue.
int sp_record_capture(struct sp_recorder *r,const uint8_t *pixels,int width,int height,int stride,int format) {
    if(!r || !pixels || width<2 || height<2 || (int64_t)width*height>32000000 || stride<width*4 || format<1 || format>4)return -1;
    if(!r->active)return sp_record_frame(r,pixels,width,height,stride,format);
    if(r->finished || r->paused)return 0;
    if(r->failed)return -1;
    if(width!=r->source_width||height!=r->source_height)return fail(r,AVERROR(EINVAL),"Capture dimensions changed");
    if(!r->preview || r->preview_width!=width || r->preview_height!=height) {
        av_freep(&r->preview);r->preview=av_malloc((size_t)width*height*4);
        if(!r->preview)return fail(r,AVERROR(ENOMEM),"Cache capture frame");
    }
    for(int y=0;y<height;y++)memcpy(r->preview+(size_t)y*width*4,pixels+(size_t)y*stride,(size_t)width*4);
    r->preview_width=width;r->preview_height=height;r->preview_format=format;
    r->pending=1;r->captured++;return 0;
}
static int drain(struct sp_recorder *r) {
    int result;
    while((result=avcodec_receive_packet(r->codec,r->packet))>=0) {
        av_packet_rescale_ts(r->packet,r->codec->time_base,r->stream->time_base);
        r->packet->stream_index=r->stream->index;
        result=av_interleaved_write_frame(r->output,r->packet);av_packet_unref(r->packet);
        if(result<0)return fail(r,result,"Write video (check disk space)");
    }
    return result==AVERROR(EAGAIN)||result==AVERROR_EOF?0:fail(r,result,"Receive encoded frame");
}
static int64_t elapsed(struct sp_recorder *r,int64_t now) {
    if(!r->active && !r->finished)return 0;
    return (r->paused?r->pause_start:now)-r->start-r->paused_us;
}
int sp_record_tick(struct sp_recorder *r,int64_t now) {
    if(!r || !r->active || r->paused)return 0;
    if(r->failed)return -1;
    if(r->pending) {
        int result=sp_record_frame(r,r->preview,r->preview_width,r->preview_height,r->preview_width*4,r->preview_format);
        if(result<0)return result;
        r->captured--;r->pending=0; // Already counted when the source delivered it.
    }
    if(!r->ready)return 0;
    // Encoder initialization may take seconds. Start the media clock at the
    // first submitted frame rather than charging initialization to the clip.
    if(r->last_pts<0)r->start=now;
    int64_t pts=av_rescale(elapsed(r,now),r->options.fps,1000000);
    if(pts<=r->last_pts)return 0;
    if(r->last_pts>=0 && pts>r->last_pts+1)r->missed+=pts-r->last_pts-1;
    int64_t started=av_gettime_relative();
    r->frame->pts=pts;AVFrame *frame=r->frame;
    if(r->hardware) {
        int result=av_frame_make_writable(r->hardware);if(result<0)return fail(r,result,"Prepare hardware frame");
        result=av_hwframe_transfer_data(r->hardware,r->frame,0);if(result<0)return fail(r,result,"Upload pixels");
        r->hardware->pts=pts;frame=r->hardware;
    }
    int result=avcodec_send_frame(r->codec,frame);if(result<0)return fail(r,result,"Encode frame");
    r->last_pts=pts;r->frames++;
    if(r->encoded_capture==r->captured)r->repeated++;
    r->encoded_capture=r->captured;
    result=drain(r);r->encode_us+=av_gettime_relative()-started;
    return result;
}
int sp_record_pause(struct sp_recorder *r,int paused,int64_t now) {
    if(!r||!r->active)return -1;
    if(paused&&!r->paused){if(r->last_pts<0 && sp_record_tick(r,now)<0)return -1;r->pause_start=now;r->paused=1;}
    if(!paused&&r->paused){r->paused_us+=now-r->pause_start;r->paused=0;}
    return 0;
}
int sp_record_finish(struct sp_recorder *r,int64_t now) {
    if(!r||!r->active)return -1;
    // The timer owns frame submission. Finishing must not append an extra
    // frame at the end boundary (or extend clips by one frame on every pause).
    if(r->last_pts<0 && sp_record_tick(r,now)<0)return -1;
    if(r->failed)return -1;
    int result=avcodec_send_frame(r->codec,NULL);if(result<0)return fail(r,result,"Flush encoder");
    if(drain(r)<0)return -1;
    result=av_write_trailer(r->output);if(result<0)return fail(r,result,"Finish MP4");
    result=avio_closep(&r->output->pb);if(result<0)return fail(r,result,"Close MP4");
    if(!r->frames)return fail(r,AVERROR(EINVAL),"No captured frames");
    if(!r->paused)r->pause_start=now;
    r->paused=1;r->active=0;r->finished=1;
    fprintf(stderr,"Recording timings: convert %.2f ms/frame, encode %.2f ms/frame\n",r->captured?(double)r->convert_us/r->captured/1000:0,r->frames?(double)r->encode_us/r->frames/1000:0);
    return 0;
}
int sp_record_active(const struct sp_recorder *r){return r&&r->active;}
const char *sp_record_error(const struct sp_recorder *r){return r?r->error:"Recorder allocation failed";}
void sp_record_status(struct sp_recorder *r,int64_t now,char *json,unsigned size) {
    snprintf(json,size,"{\"active\":%s,\"paused\":%s,\"finished\":%s,\"elapsedMs\":%lld,\"frames\":%lld,\"missed\":%lld,\"captured\":%lld,\"repeated\":%lld,\"fps\":%d,\"width\":%d,\"height\":%d,\"encoder\":\"%s\"}",
        r->active?"true":"false",r->paused?"true":"false",r->finished?"true":"false",(long long)(elapsed(r,now)/1000),(long long)r->frames,(long long)r->missed,(long long)r->captured,(long long)r->repeated,r->options.fps,r->codec?r->codec->width:0,r->codec?r->codec->height:0,r->encoder?r->encoder:"");
}
void sp_record_free(struct sp_recorder *r) {
    if(!r)return;
    if(r->output){if(r->output->pb)avio_closep(&r->output->pb);avformat_free_context(r->output);}
    release_codec(r);av_frame_free(&r->frame);av_packet_free(&r->packet);av_freep(&r->preview);sws_freeContext(r->scale);free(r);
}
