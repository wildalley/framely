#include "recorder.h"
#include <assert.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
int main(int argc,char **argv) {
    if(argc<3||argc>4)return 2;
    if(argc==4 && !strcmp(argv[3],"failure")) {
        unsigned char pixels[64*64*4]={0};
        struct sp_recorder *bad=sp_record_new((struct sp_record_options){argv[1],60,1920,"mpeg4"});assert(bad);
        assert(sp_record_capture(bad,pixels,64,64,64*4,1)==0);
        int result=sp_record_begin(bad,64,64,1000000);
        if(!result)for(int i=0;i<180&&!result;i++)result=sp_record_tick(bad,1000000+(int64_t)i*1000000/60);
        if(!result)result=sp_record_finish(bad,4000000);
        assert(result<0);assert(*sp_record_error(bad));sp_record_free(bad);puts("write failure surfaced");return 0;
    }
    if(argc==4 && !strcmp(argv[3],"crop")) {
        unsigned char pixels[80*80*4];memset(pixels,180,sizeof(pixels));
        struct sp_recorder *crop=sp_record_new((struct sp_record_options){argv[1],60,1920,argv[2]});assert(crop);
        assert(sp_record_capture(crop,pixels,80,80,80*4,1)==0);
        assert(sp_record_select(crop,16,8,64,64,80,80)==0);
        assert(sp_record_begin(crop,64,64,1000000)==0);
        for(int i=0;i<60;i++)assert(sp_record_tick(crop,1000000+(int64_t)i*1000000/60)==0);
        assert(sp_record_pause(crop,1,2000000)==0);assert(sp_record_finish(crop,7000000)==0);
        char status[512];sp_record_status(crop,7000000,status,sizeof(status));puts(status);
        sp_record_free(crop);return 0;
    }
    const int w=1920,h=1080;
    unsigned char *pixels=malloc((size_t)w*h*4);assert(pixels);
    memset(pixels,128,(size_t)w*h*4);
    struct sp_record_options options={argv[1],60,1920,argv[2]};
    struct sp_recorder *r=sp_record_new(options);assert(r);
    assert(sp_record_frame(r,pixels,w,h,w*4,1)==0);
    if(sp_record_begin(r,w,h,1000000)<0){fprintf(stderr,"%s\n",sp_record_error(r));return 1;}
    int count=argc==4&&!strcmp(argv[3],"stress")?3600:120;
    for(int i=0;i<count;i++) {
        int64_t now=1000000+(int64_t)i*1000000/60;
        if(i==60){assert(sp_record_pause(r,1,now)==0);assert(sp_record_pause(r,0,now+5000000)==0);}
        if(i>=60)now+=5000000;
        pixels[(size_t)(i*13%w)*4]=(unsigned char)i;
        assert(sp_record_capture(r,pixels,w,h,w*4,1)==0);
        assert(sp_record_tick(r,now)==0);
    }
    int64_t end=1000000+(int64_t)count*1000000/60+5000000;
    assert(sp_record_finish(r,end)==0);
    char status[512];sp_record_status(r,end,status,sizeof(status));puts(status);
    sp_record_free(r);free(pixels);return 0;
}
