// Test real mapping and frame output with a simulated DMA synchronization ioctl.
// No desktop service, GPU or permission dialog is involved.
#define ioctl capture_test_ioctl
#define pw_stream_queue_buffer capture_test_queue
#define pw_main_loop_quit capture_test_quit
#define main capture_worker_main
#include "pipewire.c"
#undef main
#include <assert.h>
#pragma GCC diagnostic push
#pragma GCC diagnostic ignored "-Wunused-parameter"
#include <spa/pod/filter.h>
#pragma GCC diagnostic pop

static unsigned sync_calls, queued, quit_calls;
int capture_test_ioctl(int fd,unsigned long request,...) {
    (void)fd;assert(request==DMA_BUF_IOCTL_SYNC);sync_calls++;return 0;
}
int capture_test_queue(struct pw_stream *stream,struct pw_buffer *buffer) {
    (void)stream;(void)buffer;queued++;return 0;
}
int capture_test_quit(struct pw_main_loop *loop) { (void)loop;quit_calls++;return 0; }

int main(void) {
    // A CPU-only source must not inherit the DMA modifier from our offer.
    // Exercise SPA's real negotiation rather than just inspecting a flag.
    uint8_t offers[1024],filtered[2048];
    struct spa_pod_builder offer_builder=SPA_POD_BUILDER_INIT(offers,sizeof(offers));
    const struct spa_pod *formats[2];capture_formats(&offer_builder,formats);
    struct spa_pod_builder filter_builder=SPA_POD_BUILDER_INIT(filtered,sizeof(filtered));
    struct spa_pod *negotiated=NULL;
    assert(spa_pod_filter(&filter_builder,&negotiated,formats[1],formats[0])<0);
    filter_builder=(struct spa_pod_builder)SPA_POD_BUILDER_INIT(filtered,sizeof(filtered));
    assert(spa_pod_filter(&filter_builder,&negotiated,formats[1],formats[1])==0);
    assert(spa_pod_find_prop(negotiated,NULL,SPA_FORMAT_VIDEO_modifier)==NULL);
    filter_builder=(struct spa_pod_builder)SPA_POD_BUILDER_INIT(filtered,sizeof(filtered));
    assert(spa_pod_filter(&filter_builder,&negotiated,formats[0],formats[0])==0);
    assert(spa_pod_find_prop(negotiated,NULL,SPA_FORMAT_VIDEO_modifier)!=NULL);
    FILE *backing=tmpfile(),*output=tmpfile();assert(backing&&output);
    int saved_stdout=dup(STDOUT_FILENO);assert(saved_stdout>=0);
    assert(dup2(fileno(output),STDOUT_FILENO)>=0);
    uint8_t row[20];for(unsigned i=0;i<sizeof(row);i++)row[i]=(uint8_t)i;
    assert(fwrite(row,1,sizeof(row),backing)==sizeof(row));
    assert(fwrite(row,1,sizeof(row),backing)==sizeof(row));fflush(backing);
    struct spa_chunk chunk={.offset=0,.size=1,.stride=20};
    struct spa_data data={.type=SPA_DATA_DmaBuf,.fd=fileno(backing),.maxsize=1,.chunk=&chunk};
    struct spa_buffer buffer={.n_datas=1,.datas=&data};
    struct pw_buffer pw={.buffer=&buffer};
    struct capture capture={.format={.format=SPA_VIDEO_FORMAT_BGRx,.modifier=0,.size={4,2}},.has_modifier=true};
    emit_buffer(&capture,&pw);
    assert(sync_calls==2&&queued==1&&quit_calls==0&&!capture.failed);
    rewind(output);uint32_t header[4];uint8_t pixels[32];
    assert(fread(header,1,sizeof(header),output)==sizeof(header));
    assert(header[0]==0x31465053&&header[1]==4&&header[2]==2&&header[3]==3);
    assert(fread(pixels,1,sizeof(pixels),output)==sizeof(pixels));
    assert(!memcmp(pixels,row,16)&&!memcmp(pixels+16,row,16));
    assert(fgetc(output)==EOF);
    // A truncated backing fd must stop capture, never read beyond its storage.
    assert(ftruncate(fileno(backing),8)==0);
    emit_buffer(&capture,&pw);
    assert(capture.failed&&quit_calls==1&&queued==2&&sync_calls==4);
    assert(dup2(saved_stdout,STDOUT_FILENO)>=0);close(saved_stdout);
    fclose(output);fclose(backing);
    puts("PipeWire linear DMA-BUF sentinel, stride, output and bounds checks passed");
    return 0;
}
