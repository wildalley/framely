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
    const struct spa_pod *formats[2];capture_formats(&offer_builder,formats,0);
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
    // Cropping must honor the original row stride and retain source metadata.
    assert(ftruncate(fileno(output),0)==0);rewind(output);
    capture.protocol=2;
    assert(sp_parse_crop("crop 7 1 1 2 1 4 2",4,2,&capture.region)==0);
    emit_buffer(&capture,&pw);
    rewind(output);uint32_t cropped_header[9];uint8_t cropped[8];
    assert(fread(cropped_header,1,sizeof(cropped_header),output)==sizeof(cropped_header));
    const uint32_t expected[]={0x32465053,2,1,3,1,1,4,2,7};
    assert(!memcmp(cropped_header,expected,sizeof(expected)));
    assert(fread(cropped,1,sizeof(cropped),output)==sizeof(cropped));
    assert(!memcmp(cropped,row+4,8)&&fgetc(output)==EOF);
    struct sp_region rejected;
    const char *bad[]={"crop 1 -1 0 1 1 4 2","crop 1 3 0 2 1 4 2","crop 1 0 0 0 1 4 2","crop 1 0 0 1 1 5 2","crop 1 0 0 4294967297 1 4 2","crop 1 0 0 1 1 4 2 extra"};
    for(unsigned i=0;i<sizeof(bad)/sizeof(bad[0]);i++)assert(sp_parse_crop(bad[i],4,2,&rejected)<0);
    // A CPU buffer can start at a nonzero chunk offset. The selected last
    // row must stop at the last pixel, without requiring stride padding.
    assert(ftruncate(fileno(output),0)==0);rewind(output);
    uint8_t cpu[43];memset(cpu,0xEE,sizeof(cpu));memcpy(cpu+7,row,20);memcpy(cpu+27,row,16);
    struct spa_chunk cpu_chunk={.offset=7,.size=36,.stride=20};
    struct spa_data cpu_data={.type=SPA_DATA_MemPtr,.data=cpu,.maxsize=sizeof(cpu),.chunk=&cpu_chunk};
    buffer.datas=&cpu_data;
    command_received(&capture,"crop 8 3 1 1 1 4 2");
    emit_buffer(&capture,&pw);
    rewind(output);uint32_t reply_header[4];
    assert(fread(reply_header,1,sizeof(reply_header),output)==sizeof(reply_header));
    assert(reply_header[0]==0x31435053&&reply_header[1]==0&&reply_header[2]==0&&reply_header[3]==8);
    assert(fread(cropped_header,1,sizeof(cropped_header),output)==sizeof(cropped_header));
    assert(cropped_header[1]==1&&cropped_header[2]==1&&cropped_header[4]==3&&cropped_header[5]==1&&cropped_header[8]==8);
    assert(fread(cropped,1,4,output)==4&&!memcmp(cropped,row+12,4)&&fgetc(output)==EOF);
    // A truncated CPU buffer emits no packet and cannot read out of bounds.
    assert(ftruncate(fileno(output),0)==0);rewind(output);cpu_data.maxsize--;
    emit_buffer(&capture,&pw);rewind(output);assert(fgetc(output)==EOF);
    buffer.datas=&data;
    // A truncated backing fd must stop capture, never read beyond its storage.
    assert(ftruncate(fileno(backing),8)==0);
    emit_buffer(&capture,&pw);
    assert(capture.failed&&quit_calls==1&&queued==5&&sync_calls==6);
    assert(dup2(saved_stdout,STDOUT_FILENO)>=0);close(saved_stdout);
    fclose(output);fclose(backing);
    puts("PipeWire DMA-BUF/CPU crop, acknowledgment, stride and bounds checks passed");
    return 0;
}
