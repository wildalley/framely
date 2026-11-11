// Windows Graphics Capture CPU-frame bridge, Windows 10 1903+.
// Build with MSVC and the Windows SDK (C++/WinRT); no injected DLLs/hooks.
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <shellapi.h>
#include <d3d11.h>
#include <dxgi1_2.h>
#include <windows.graphics.capture.interop.h>
#include <windows.graphics.directx.direct3d11.interop.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Graphics.Capture.h>
#include <winrt/Windows.Graphics.DirectX.h>
#include <winrt/Windows.Graphics.DirectX.Direct3D11.h>
#include <atomic>
#include <chrono>
#include <cstdint>
#include <cstdlib>
#include <iostream>
#include <memory>
#include <mutex>
#include <sstream>
#include <string>
#include <stdexcept>
#include <thread>
#include <vector>
#include "frame_protocol.h"
#ifdef SCREENPIN_RECORD
#include "../media/recorder.h"
static int64_t recordNow(){return std::chrono::duration_cast<std::chrono::microseconds>(std::chrono::steady_clock::now().time_since_epoch()).count();}
#endif

using namespace winrt;
using namespace winrt::Windows::Graphics::Capture;
using namespace winrt::Windows::Graphics::DirectX;
using namespace winrt::Windows::Graphics::DirectX::Direct3D11;

struct Monitor { HMONITOR handle; MONITORINFOEXW info; bool underMouse; };
static std::mutex outputMutex;
static bool writeAll(const void* data, size_t length) {
    auto p=static_cast<const uint8_t*>(data);
    while(length) { DWORD written=0; if(!WriteFile(GetStdHandle(STD_OUTPUT_HANDLE),p,static_cast<DWORD>(length),&written,nullptr)||!written)return false; p+=written;length-=written; }
    return true;
}
static void replyLocked(uint32_t id,const std::string& error) {
    uint32_t header[]={0x31435053,static_cast<uint32_t>(error.size()),error.empty()?0u:1u,id};
    writeAll(header,sizeof(header));if(!error.empty())writeAll(error.data(),error.size());
}
static void reply(uint32_t id,const std::string& error) {std::lock_guard<std::mutex> lock(outputMutex);replyLocked(id,error);}
static std::vector<Monitor> monitors() {
    std::vector<Monitor> list; POINT mouse{};GetCursorPos(&mouse);HMONITOR under=MonitorFromPoint(mouse,MONITOR_DEFAULTTONEAREST);
    EnumDisplayMonitors(nullptr,nullptr,[](HMONITOR monitor,HDC,LPRECT,LPARAM arg)->BOOL {
        auto list=reinterpret_cast<std::vector<Monitor>*>(arg);MONITORINFOEXW info{};info.cbSize=sizeof(info);
        if(GetMonitorInfoW(monitor,reinterpret_cast<MONITORINFO*>(&info)))list->push_back({monitor,info,false});return TRUE;
    },reinterpret_cast<LPARAM>(&list));
    for(auto& m:list)m.underMouse=m.handle==under;return list;
}

struct InputControl {
#ifdef SCREENPIN_RECORD
    std::shared_ptr<sp_recorder> recording;
    bool recordingStarted=false;
    int fps=60;
    int64_t statusSent=0;
#endif
    RECT monitor; HMONITOR handle; DWORD owner; HWND target=nullptr; POINT last{};
    std::atomic<bool> stop{false};
    struct sp_region region{}; // Read/write only while holding outputMutex.
    uint32_t sourceWidth=0,sourceHeight=0;
    std::string scroll(int x,int y) {
        MONITORINFO current{};current.cbSize=sizeof(current);
        if(!GetMonitorInfoW(handle,&current)||!EqualRect(&current.rcMonitor,&monitor))return "屏幕布局已变化，请重新开始长截图";
        if(x<0||y<0||x>=monitor.right-monitor.left||y>=monitor.bottom-monitor.top)return "滚动位置超出屏幕";
        POINT point{monitor.left+x,monitor.top+y},cursor{};GetCursorPos(&cursor);
        if(target && (std::abs(cursor.x-last.x)>48||std::abs(cursor.y-last.y)>48)) {target=nullptr;return "鼠标已移开，自动滚动暂停；可手动滚动或重新启用自动模式";}
        HWND candidate=GetAncestor(WindowFromPoint(point),GA_ROOT);DWORD pid=0;GetWindowThreadProcessId(candidate,&pid);
        if(!candidate||pid==owner||pid==GetCurrentProcessId())return "滚动区域被 Framely 窗口遮挡，请移动或隐藏窗口";
        if(target && candidate!=target){target=nullptr;return "目标窗口已变化，自动滚动暂停";}
        if(GetForegroundWindow()!=candidate && !SetForegroundWindow(candidate)){target=nullptr;return "无法激活目标窗口，请先点击它或使用手动滚动";}
        if(!SetCursorPos(point.x,point.y)){target=nullptr;return "无法定位滚动位置";}
        INPUT input{};input.type=INPUT_MOUSE;input.mi.dwFlags=MOUSEEVENTF_WHEEL;input.mi.mouseData=static_cast<DWORD>(-WHEEL_DELTA);
        if(SendInput(1,&input,sizeof(input))!=1){target=nullptr;return "Windows 拒绝了滚轮输入，目标程序可能以管理员权限运行；可使用手动滚动";}
        target=candidate;last=point;return {};
    }
};

int main(int argc,char** argv) {
    // CRT narrow argv follows the system code page; FFmpeg paths are UTF-8.
    // Keep recordings named in Chinese (and other Unicode paths) lossless.
    int count=0;LPWSTR *wide=CommandLineToArgvW(GetCommandLineW(),&count);
    if(!wide)return 2;
    std::vector<std::string> utf8;utf8.reserve(count);
    for(int i=0;i<count;i++)utf8.push_back(to_string(wide[i]));
    LocalFree(wide);
    std::vector<char*> arguments;for(auto& arg:utf8)arguments.push_back(arg.data());
    arguments.push_back(nullptr);argc=count;argv=arguments.data();
    if(argc==2&&std::string(argv[1])=="--protocol-version"){std::cout<<"2\n";return 0;}
    SetProcessDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2);
    try {
        auto screens=monitors();
        if(argc==2 && std::string(argv[1])=="--list") {
            std::cout<<"[";
            for(size_t i=0;i<screens.size();i++) {if(i)std::cout<<",";auto r=screens[i].info.rcMonitor;
                std::cout<<"{\"id\":"<<i+1<<",\"name\":\"Display "<<i+1<<" ("<<r.right-r.left<<"x"<<r.bottom-r.top<<")\",\"underMouse\":"<<(screens[i].underMouse?"true":"false")<<"}";}
            std::cout<<"]";return 0;
        }
        int selected=0,protocol=1;DWORD owner=0;
#ifdef SCREENPIN_RECORD
        sp_record_options recording{nullptr,60,1920,"auto"};
#endif
        for(int i=1;i+1<argc;i+=2) {if(std::string(argv[i])=="--monitor")selected=std::atoi(argv[i+1]);else if(std::string(argv[i])=="--owner-pid")owner=std::strtoul(argv[i+1],nullptr,10);else if(std::string(argv[i])=="--protocol")protocol=std::atoi(argv[i+1]);
#ifdef SCREENPIN_RECORD
            else if(std::string(argv[i])=="--record")recording.path=argv[i+1];
            else if(std::string(argv[i])=="--fps")recording.fps=std::atoi(argv[i+1]);
            else if(std::string(argv[i])=="--max-edge")recording.max_edge=std::atoi(argv[i+1]);
            else if(std::string(argv[i])=="--codec")recording.codec=argv[i+1];
#endif
            else return 2;}
        if(protocol!=1&&protocol!=2)return 2;
        if(screens.empty())throw std::runtime_error("No displays are available");
        if(selected==0){for(size_t i=0;i<screens.size();i++)if(screens[i].underMouse)selected=static_cast<int>(i)+1;}
        if(selected<1||static_cast<size_t>(selected)>screens.size())throw std::runtime_error("Selected display is unavailable");
        auto monitor=screens[selected-1];
        init_apartment(apartment_type::multi_threaded);
        if(!GraphicsCaptureSession::IsSupported())throw std::runtime_error("Windows Graphics Capture is unavailable");
        com_ptr<ID3D11Device> device;com_ptr<ID3D11DeviceContext> context;
        HRESULT hr=D3D11CreateDevice(nullptr,D3D_DRIVER_TYPE_HARDWARE,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,nullptr,0,D3D11_SDK_VERSION,device.put(),nullptr,context.put());
        if(FAILED(hr))hr=D3D11CreateDevice(nullptr,D3D_DRIVER_TYPE_WARP,nullptr,D3D11_CREATE_DEVICE_BGRA_SUPPORT,nullptr,0,D3D11_SDK_VERSION,device.put(),nullptr,context.put());
        check_hresult(hr);
        auto dxgi=device.as<IDXGIDevice>();com_ptr<::IInspectable> inspectable;
        check_hresult(CreateDirect3D11DeviceFromDXGIDevice(dxgi.get(),inspectable.put()));
        auto graphicsDevice=inspectable.as<IDirect3DDevice>();
        GraphicsCaptureItem item{nullptr};auto interop=get_activation_factory<GraphicsCaptureItem,IGraphicsCaptureItemInterop>();
        check_hresult(interop->CreateForMonitor(monitor.handle,guid_of<GraphicsCaptureItem>(),put_abi(item)));
        auto size=item.Size();
        if(size.Width<=0||size.Height<=0||size.Width>16384||size.Height>16384||static_cast<uint64_t>(size.Width)*size.Height>32000000)throw std::runtime_error("Display exceeds capture limits");
        auto pool=Direct3D11CaptureFramePool::CreateFreeThreaded(graphicsDevice,DirectXPixelFormat::B8G8R8A8UIntNormalized,2,size);
        auto session=pool.CreateCaptureSession(item);
        try{session.IsCursorCaptureEnabled(false);}catch(hresult_error const&){}
        session.StartCapture();
        auto control=std::make_shared<InputControl>();control->monitor=monitor.info.rcMonitor;control->handle=monitor.handle;control->owner=owner;
#ifdef SCREENPIN_RECORD
        control->recording=std::shared_ptr<sp_recorder>(sp_record_new(recording),sp_record_free);
        if(!control->recording)throw std::runtime_error("Invalid recording options");
        control->fps=recording.fps;
        try{session.IsCursorCaptureEnabled(true);}catch(hresult_error const&){}
#endif
        control->sourceWidth=static_cast<uint32_t>(size.Width);control->sourceHeight=static_cast<uint32_t>(size.Height);
        std::thread([control,protocol] {
            char line[256];
            while(std::cin.getline(line,sizeof(line))) {
                if(protocol==2&&!strncmp(line,"crop ",5)) {
                    std::lock_guard<std::mutex> lock(outputMutex);struct sp_region next{};
                    if(sp_parse_crop(line,control->sourceWidth,control->sourceHeight,&next)<0)replyLocked(next.generation,"Invalid crop or changed source size");
#ifdef SCREENPIN_RECORD
                    else if(control->recordingStarted || sp_record_select(control->recording.get(),next.x,next.y,next.width,next.height,control->sourceWidth,control->sourceHeight)<0)replyLocked(next.generation,"Cannot change recording region");
#endif
                    else {control->region=next;replyLocked(next.generation,{});}
                    continue;
                }
                std::istringstream input(line);std::string command;uint32_t id;int x,y;
#ifdef SCREENPIN_RECORD
                if(input>>command>>id) {
                    std::lock_guard<std::mutex> lock(outputMutex);
                    int result=-1;auto r=control->recording.get();
                    if(command=="record"&&control->region.generation){result=sp_record_begin(r,control->region.width,control->region.height,recordNow());if(!result)control->recordingStarted=true;}
                    else if(command=="pause")result=sp_record_pause(r,1,recordNow());
                    else if(command=="resume")result=sp_record_pause(r,0,recordNow());
                    else if(command=="finish")result=sp_record_finish(r,recordNow());
                    if(!result){char status[512];sp_record_status(r,recordNow(),status,sizeof(status));uint32_t h[]={0x31525053,static_cast<uint32_t>(strlen(status)),0,0};if(!writeAll(h,sizeof(h))||!writeAll(status,strlen(status)))control->stop=true;}
                    replyLocked(id,result<0?(*sp_record_error(r)?sp_record_error(r):"Invalid recording state"):"");
                }
#else
                if(input>>command>>id>>x>>y && command=="scroll")reply(id,control->scroll(x,y));
#endif
            }
            control->stop=true;
        }).detach();
        com_ptr<ID3D11Texture2D> staging;
        uint32_t stagingWidth=0,stagingHeight=0;
        auto last=std::chrono::steady_clock::now()-std::chrono::seconds(1);
        while(!control->stop) {
#ifdef SCREENPIN_RECORD
            {
                std::lock_guard<std::mutex> lock(outputMutex);auto r=control->recording.get();auto stamp=recordNow();
                if(sp_record_tick(r,stamp)<0)throw std::runtime_error(sp_record_error(r));
                if(control->recordingStarted&&stamp-control->statusSent>=250000) {
                    char status[512];sp_record_status(r,stamp,status,sizeof(status));control->statusSent=stamp;
                    uint32_t header[]={0x31525053,static_cast<uint32_t>(strlen(status)),0,0};
                    if(!writeAll(header,sizeof(header))||!writeAll(status,strlen(status)))break;
                }
            }
#endif
            auto frame=pool.TryGetNextFrame();if(!frame){
#ifdef SCREENPIN_RECORD
                std::this_thread::sleep_for(std::chrono::milliseconds(2));
#else
                std::this_thread::sleep_for(std::chrono::milliseconds(20));
#endif
                continue;
            }
            auto content=frame.ContentSize();if(content.Width!=size.Width||content.Height!=size.Height)throw std::runtime_error("Display size changed; start capture again");
            int interval=120000;
#ifdef SCREENPIN_RECORD
            {std::lock_guard<std::mutex> lock(outputMutex);if(control->recordingStarted)interval=1000000/control->fps;}
#endif
            auto now=std::chrono::steady_clock::now();if(now-last<std::chrono::microseconds(interval)){frame.Close();continue;}
#ifdef SCREENPIN_RECORD
            if(now-last<std::chrono::microseconds(interval*2))last+=std::chrono::microseconds(interval);else last=now;
#else
            last=now;
#endif
            auto access=frame.Surface().as<::Windows::Graphics::DirectX::Direct3D11::IDirect3DDxgiInterfaceAccess>();com_ptr<ID3D11Texture2D> texture;
            check_hresult(access->GetInterface(__uuidof(ID3D11Texture2D),texture.put_void()));
            // Serialize crop acknowledgment with frame emission. After its
            // reply, no older-region frame can enter the output stream.
            std::unique_lock<std::mutex> outputLock(outputMutex);
            auto region=control->region;
            uint32_t x=region.generation?region.x:0,y0=region.generation?region.y:0;
            uint32_t width=region.generation?region.width:static_cast<uint32_t>(size.Width),height=region.generation?region.height:static_cast<uint32_t>(size.Height);
            if(!staging||stagingWidth!=width||stagingHeight!=height){staging=nullptr;D3D11_TEXTURE2D_DESC desc{};texture->GetDesc(&desc);desc.Width=width;desc.Height=height;desc.Usage=D3D11_USAGE_STAGING;desc.BindFlags=0;desc.MiscFlags=0;desc.CPUAccessFlags=D3D11_CPU_ACCESS_READ;check_hresult(device->CreateTexture2D(&desc,nullptr,staging.put()));stagingWidth=width;stagingHeight=height;}
            D3D11_BOX box{x,y0,0,x+width,y0+height,1};
            context->CopySubresourceRegion(staging.get(),0,0,0,0,texture.get(),0,&box);
            D3D11_MAPPED_SUBRESOURCE map{};check_hresult(context->Map(staging.get(),0,D3D11_MAP_READ,0,&map));
            bool okay=true;
#ifdef SCREENPIN_RECORD
            auto pixels=static_cast<const uint8_t*>(map.pData);
            int result=sp_record_capture(control->recording.get(),pixels,width,height,map.RowPitch,1);
            if(result<0){context->Unmap(staging.get(),0);throw std::runtime_error(sp_record_error(control->recording.get()));}
            if(!control->recordingStarted)
#endif
            {uint32_t header[]={protocol==2?0x32465053u:0x31465053u,width,height,1,x,y0,static_cast<uint32_t>(size.Width),static_cast<uint32_t>(size.Height),region.generation};
                okay=writeAll(header,protocol==2?sizeof(header):16);
                for(uint32_t y=0;okay&&y<height;y++)okay=writeAll(static_cast<const uint8_t*>(map.pData)+static_cast<size_t>(y)*map.RowPitch,static_cast<size_t>(width)*4);}
            context->Unmap(staging.get(),0);frame.Close();if(!okay)break;
        }
#ifdef SCREENPIN_RECORD
        {std::lock_guard<std::mutex> lock(outputMutex);auto r=control->recording.get();if(sp_record_active(r)&&sp_record_finish(r,recordNow())<0)std::cerr<<sp_record_error(r)<<"\n";}
#endif
        session.Close();pool.Close();return 0;
    }catch(hresult_error const& error){std::cerr<<to_string(error.message())<<" (Windows Graphics Capture requires Windows 10 1903+)\n";return 1;}
    catch(std::exception const& error){std::cerr<<error.what()<<"\n";return 1;}
}
