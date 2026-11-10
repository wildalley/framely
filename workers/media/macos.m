// ScreenCaptureKit source for the native recorder (macOS 15+).
#import <Cocoa/Cocoa.h>
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#include <stdatomic.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <unistd.h>
#include <signal.h>
#include "recorder.h"
#include "../capture/frame_protocol.h"

static int64_t clock_us(void){return (int64_t)(NSProcessInfo.processInfo.systemUptime*1000000);}
static BOOL write_bytes(const void *data,size_t size){const uint8_t *p=data;while(size){ssize_t n=write(1,p,size);if(n<0&&errno==EINTR)continue;if(n<=0)return NO;p+=n;size-=n;}return YES;}
@interface Recorder : NSObject <SCStreamOutput,SCStreamDelegate> {
@public
 struct sp_recorder *encoder;
 struct sp_region region;
 SCStream *stream;
 dispatch_queue_t queue;
 dispatch_source_t timer;
 int width,height,fps;
 BOOL started;
 atomic_bool stopped;
 int64_t statusSent,previewSent;
}
@end
@implementation Recorder
- (void)stream:(SCStream *)s didStopWithError:(NSError *)error { (void)s;fprintf(stderr,"ScreenCaptureKit: %s\n",error.localizedDescription.UTF8String);atomic_store(&stopped,true); }
- (void)stream:(SCStream *)s didOutputSampleBuffer:(CMSampleBufferRef)buffer ofType:(SCStreamOutputType)type {
 (void)s;if(atomic_load(&stopped)||!encoder)return;if(type!=SCStreamOutputTypeScreen||!CMSampleBufferIsValid(buffer))return;
 CVPixelBufferRef pixels=CMSampleBufferGetImageBuffer(buffer);if(!pixels)return;
 int w=(int)CVPixelBufferGetWidth(pixels),h=(int)CVPixelBufferGetHeight(pixels);
 if(w!=width||h!=height){fprintf(stderr,"Capture dimensions changed\n");atomic_store(&stopped,true);return;}
 int64_t now=clock_us();if(!started&&now-previewSent<100000)return;previewSent=now;
 CVPixelBufferLockBaseAddress(pixels,kCVPixelBufferLock_ReadOnly);
 const uint8_t *p=CVPixelBufferGetBaseAddress(pixels);int stride=(int)CVPixelBufferGetBytesPerRow(pixels);
 int x=region.generation?region.x:0,y=region.generation?region.y:0;
 int rw=region.generation?region.width:w,rh=region.generation?region.height:h;
 p+=(size_t)y*stride+(size_t)x*4;
 if(sp_record_capture(encoder,p,rw,rh,stride,1)<0)atomic_store(&stopped,true);
 if(!started){uint32_t header[]={0x32465053,rw,rh,1,x,y,w,h,region.generation};BOOL okay=write_bytes(header,sizeof(header));for(int row=0;okay&&row<rh;row++)okay=write_bytes(p+(size_t)row*stride,(size_t)rw*4);if(!okay)atomic_store(&stopped,true);}
 CVPixelBufferUnlockBaseAddress(pixels,kCVPixelBufferLock_ReadOnly);
}
@end
int main(int argc,char **argv) {
 @autoreleasepool {
  if(argc==2&&!strcmp(argv[1],"--protocol-version")){puts("2");return 0;}
  signal(SIGPIPE,SIG_IGN);
  struct sp_record_options options={NULL,60,1920,"auto"};unsigned displayID=0;
  for(int i=1;i+1<argc;i+=2){if(!strcmp(argv[i],"--record"))options.path=argv[i+1];else if(!strcmp(argv[i],"--fps"))options.fps=atoi(argv[i+1]);else if(!strcmp(argv[i],"--max-edge"))options.max_edge=atoi(argv[i+1]);else if(!strcmp(argv[i],"--codec"))options.codec=argv[i+1];else if(!strcmp(argv[i],"--monitor"))displayID=(unsigned)strtoul(argv[i+1],NULL,10);else if(strcmp(argv[i],"--protocol"))return 2;}
  Recorder *r=[Recorder new];r->encoder=sp_record_new(options);if(!r->encoder)return 2;r->fps=options.fps;atomic_init(&r->stopped,false);
  r->queue=dispatch_queue_create("io.screenpin.recording",DISPATCH_QUEUE_SERIAL);
  __block SCShareableContent *content=nil;__block NSError *failure=nil;__block BOOL ready=NO;
  [SCShareableContent getShareableContentExcludingDesktopWindows:NO onScreenWindowsOnly:YES completionHandler:^(SCShareableContent *c,NSError *error){dispatch_async(dispatch_get_main_queue(),^{content=c;failure=error;ready=YES;});}];
  while(!ready)[[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:.02]];
  if(failure){fprintf(stderr,"Screen sharing: %s\n",failure.localizedDescription.UTF8String);sp_record_free(r->encoder);return 1;}
  SCDisplay *display=nil;CGDirectDisplayID wanted=displayID?displayID:CGMainDisplayID();for(SCDisplay *d in content.displays)if(d.displayID==wanted){display=d;break;}
  if(!display){fprintf(stderr,"Requested display unavailable\n");sp_record_free(r->encoder);return 1;}
  r->width=(int)CGDisplayPixelsWide(display.displayID);r->height=(int)CGDisplayPixelsHigh(display.displayID);
  if(r->width<2||r->height<2||(int64_t)r->width*r->height>32000000){sp_record_free(r->encoder);return 1;}
  NSMutableArray<SCRunningApplication *> *excluded=[NSMutableArray array];
  for(SCRunningApplication *app in content.applications)if(app.processID==getppid())[excluded addObject:app];
  SCContentFilter *filter=[[SCContentFilter alloc] initWithDisplay:display excludingApplications:excluded exceptingWindows:@[]];
  SCStreamConfiguration *config=[SCStreamConfiguration new];config.width=r->width;config.height=r->height;config.pixelFormat=kCVPixelFormatType_32BGRA;config.minimumFrameInterval=CMTimeMake(1,options.fps);config.queueDepth=3;config.showsCursor=YES;config.capturesAudio=NO;
  r->stream=[[SCStream alloc] initWithFilter:filter configuration:config delegate:r];NSError *error=nil;
  if(![r->stream addStreamOutput:r type:SCStreamOutputTypeScreen sampleHandlerQueue:r->queue error:&error]){fprintf(stderr,"Output: %s\n",error.localizedDescription.UTF8String);sp_record_free(r->encoder);return 1;}
  [r->stream startCaptureWithCompletionHandler:^(NSError *e){if(e){fprintf(stderr,"Capture: %s\n",e.localizedDescription.UTF8String);atomic_store(&r->stopped,true);}}];
  r->timer=dispatch_source_create(DISPATCH_SOURCE_TYPE_TIMER,0,0,r->queue);
  dispatch_source_set_timer(r->timer,dispatch_time(DISPATCH_TIME_NOW,0),NSEC_PER_SEC/options.fps,NSEC_PER_MSEC);
  dispatch_source_set_event_handler(r->timer,^{
   if(atomic_load(&r->stopped)||!r->encoder)return;
   int64_t now=clock_us();if(sp_record_tick(r->encoder,now)<0){fprintf(stderr,"%s\n",sp_record_error(r->encoder));atomic_store(&r->stopped,true);}
   if(r->started&&now-r->statusSent>=250000){char status[512];sp_record_status(r->encoder,now,status,sizeof(status));r->statusSent=now;uint32_t header[]={0x31525053,(uint32_t)strlen(status),0,0};if(!write_bytes(header,sizeof(header))||!write_bytes(status,strlen(status)))atomic_store(&r->stopped,true);}
  });dispatch_resume(r->timer);
  dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED,0),^{
   char line[256];while(fgets(line,sizeof(line),stdin)){
    line[strcspn(line,"\r\n")]=0;NSString *command=[NSString stringWithUTF8String:line];if(!command)break;
    dispatch_sync(r->queue,^{
     if(atomic_load(&r->stopped)||!r->encoder)return;
     unsigned id=0;int result=-1;const char *input=command.UTF8String;const char *message="";
     if(!strncmp(input,"crop ",5)){struct sp_region next={0};result=sp_parse_crop(input,r->width,r->height,&next);id=next.generation;if(!result){result=r->started?-1:sp_record_select(r->encoder,next.x,next.y,next.width,next.height,r->width,r->height);if(!result)r->region=next;}}
     else if(sscanf(input,"record %u",&id)==1&&r->region.generation){result=sp_record_begin(r->encoder,r->region.width,r->region.height,clock_us());if(!result)r->started=YES;}
     else if(sscanf(input,"pause %u",&id)==1)result=sp_record_pause(r->encoder,1,clock_us());
     else if(sscanf(input,"resume %u",&id)==1)result=sp_record_pause(r->encoder,0,clock_us());
     else if(sscanf(input,"finish %u",&id)==1)result=sp_record_finish(r->encoder,clock_us());
     if(result<0)message=*sp_record_error(r->encoder)?sp_record_error(r->encoder):"Invalid recording operation";
     if(!result&&r->started){char status[512];sp_record_status(r->encoder,clock_us(),status,sizeof(status));uint32_t h[]={0x31525053,(uint32_t)strlen(status),0,0};if(!write_bytes(h,sizeof(h))||!write_bytes(status,strlen(status)))atomic_store(&r->stopped,true);}
     uint32_t header[]={0x31435053,(uint32_t)strlen(message),result<0?1u:0u,id};if(!write_bytes(header,sizeof(header))||!write_bytes(message,strlen(message)))atomic_store(&r->stopped,true);
    });
   }atomic_store(&r->stopped,true);
  });
  while(!atomic_load(&r->stopped))[[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:.02]];
  dispatch_source_cancel(r->timer);
  __block BOOL ended=NO;[r->stream stopCaptureWithCompletionHandler:^(NSError *e){(void)e;dispatch_async(dispatch_get_main_queue(),^{ended=YES;});}];
  while(!ended)[[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:.02]];
  dispatch_sync(r->queue,^{if(sp_record_active(r->encoder)&&sp_record_finish(r->encoder,clock_us())<0)fprintf(stderr,"%s\n",sp_record_error(r->encoder));sp_record_free(r->encoder);r->encoder=NULL;});return 0;
 }
}
