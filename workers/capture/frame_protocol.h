#ifndef SCREENPIN_FRAME_PROTOCOL_H
#define SCREENPIN_FRAME_PROTOCOL_H
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>

struct sp_region {
    uint32_t x,y,width,height,source_width,source_height,generation;
};

// Shared by PipeWire and WGC: never accept signed values, overflow, trailing
// fields or coordinates outside the expected, unchanged source dimensions.
static inline int sp_parse_crop(const char *line,uint32_t source_width,uint32_t source_height,struct sp_region *out) {
    uint32_t values[7]={0};
    memset(out,0,sizeof(*out));
    if(strncmp(line,"crop ",5))return -1;
    const char *p=line+5;
    for(unsigned i=0;i<7;i++) {
        while(*p==' ')p++;
        if(*p<'0'||*p>'9')return -1;
        char *end=NULL;errno=0;
        unsigned long long n=strtoull(p,&end,10);
        if(errno||n>UINT32_MAX||end==p)return -1;
        values[i]=(uint32_t)n;
        if(i==0)out->generation=values[0];
        if(i<6&&*end!=' ')return -1;
        p=end;
    }
    while(*p==' '||*p=='\r')p++;
    if(*p||!values[0]||!values[3]||!values[4]||values[5]!=source_width||values[6]!=source_height||
       values[1]>source_width||values[2]>source_height||values[3]>source_width-values[1]||values[4]>source_height-values[2])return -1;
    out->x=values[1];out->y=values[2];out->width=values[3];out->height=values[4];
    out->source_width=source_width;out->source_height=source_height;
    return 0;
}
#endif
