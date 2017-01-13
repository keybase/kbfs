#if defined(_WIN32) || defined(WIN32) || defined(__CYGWIN__) || defined(__MINGW32__) || defined(__BORLANDC__)

#define HAVE_CBFS 1
#include "binding.h"

static throw_if_needed(keybase_Cbfs_wrap *w) {
  if(w->ecode!=0)
    throw(ECBFSError(w->ecode));
}

static error_wrap(ECBFSError &err, keybase_Cbfs_wrap *w) {
  w->ecode = err.ErrorCode();
  auto msg = err.Message();
  if(msg != nullptr)
    w->emsg = wcsdup(msg);
}

EXT_C CallbackFileSystem* keybase_Cbfs_New_CallbackFileSystem(uint32_t tag, uint32_t *max_filename_length_ptr) {
  auto fs = new CallbackFileSystem;
  fs->SetTag((void*)tag);
  *max_filename_length_ptr = fs->GetMaxFileNameLength();
  return fs;
}
EXT_C void keybase_Cbfs_Delete_CallbackFileSystem(CallbackFileSystem* fs) {
  if(fs)
    delete fs;
}

#define CBSTART(x)   keybase_Cbfs_wrap w = {sender,0,nullptr,(uint32_t)sender->GetTag(), (uint32_t)((x)->get_UserContext())}
#define CBSTART0   keybase_Cbfs_wrap w = {sender,0,nullptr,(uint32_t)sender->GetTag(), 0}
#define CBEND   throw_if_needed(&w)

template<void (*T)(keybase_Cbfs_wrap*)>
static inline cbworker0(CallbackFileSystem *sender) {
  CBSTART0;
  (T)(&w);
  CBEND;
}
template<typename T, typename... Args>
static inline cbworker2(T t, CallbackFileSystem *sender, Args... args) {
  CBSTART0;
  (t)(&w, args...);
  CBEND;
}
#define CBPROTO(x, ...) \
EXT_C void keybase_cgo_Cbfs_##x(keybase_Cbfs_wrap*,##__VA_ARGS__); \
static void keybase_Cbfs_##x(CallbackFileSystem *sender
#define CBODY(x,...) ) {\
  cbworker2(keybase_cgo_Cbfs_##x,sender, ##__VA_ARGS__);\
}

#define CB0(x) CBPROTO(x)) { cbworker0<keybase_cgo_Cbfs_##x>(sender); }
#define CB1(x, t1) CBPROTO(x,t1), t1 a1 CBODY(x,a1)
#define CB2(x,t1,t2) CBPROTO(x,t1,t2),t1 a1, t2 a2 CBODY(x,a1,a2)
#define CB3(x,t1,t2,t3) CBPROTO(x,t1,t2,t3),t1 a1, t2 a2, t3 a3 CBODY(x,a1,a2, a3)
#define CB4(x,t1,t2,t3,t4) CBPROTO(x,t1,t2,t3,t4),t1 a1, t2 a2, t3 a3, t4 a4 CBODY(x,a1,a2, a3, a4)
#define CB5(x,t1,t2,t3,t4,t5) CBPROTO(x,t1,t2,t3,t4,t5),t1 a1, t2 a2, t3 a3, t4 a4,t5 a5 CBODY(x,a1,a2, a3, a4, a5)
#define CB6(x,t1,t2,t3,t4,t5,t6) CBPROTO(x,t1,t2,t3,t4,t5,t6),t1 a1, t2 a2, t3 a3, t4 a4,t5 a5,t6 a6 CBODY(x,a1,a2, a3, a4, a5, a6)
#define CB7(x,t1,t2,t3,t4,t5,t6,t7) CBPROTO(x,t1,t2,t3,t4,t5,t6,t7),t1 a1, t2 a2, t3 a3, t4 a4,t5 a5,t6 a6,t7 a7 CBODY(x,a1,a2, a3, a4, a5, a6, a7)
#define CB15(x,t1,t2,t3,t4,t5,t6,t7,t8,t9,ta,tb,tc,td,te,tf) \
CBPROTO(x,t1,t2,t3,t4,t5,t6,t7,t8,t9,ta,tb,tc,td,te,tf)\
,t1 a1, t2 a2, t3 a3, t4 a4,t5 a5,t6 a6,t7 a7,t8 a8,t9 a9,ta aa,tb ab,tc ac,td ad,te ae,tf af\
 CBODY(x,a1,a2, a3, a4, a5, a6, a7, a8,a9,aa,ab,ac,ad,ae,af)
#define CB19(x,t1,t2,t3,t4,t5,t6,t7,t8,t9,ta,tb,tc,td,te,tf,tg,th,ti,tj) \
CBPROTO(x,t1,t2,t3,t4,t5,t6,t7,t8,t9,ta,tb,tc,td,te,tf,tg,th,ti,tj)\
,t1 a1, t2 a2, t3 a3, t4 a4,t5 a5,t6 a6,t7 a7,t8 a8,t9 a9,ta aa,tb ab,tc ac,td ad,te ae,tf af\
,tg ag,th ah,ti ai,tj aj\
 CBODY(x,a1,a2, a3, a4, a5, a6, a7, a8,a9,aa,ab,ac,ad,ae,af,ag,ah,ai,aj)

CB0(Mount)
CB0(Unmount)
CB2(GetVolumeSize, __int64*,__int64*)
CB1(GetVolumeLabelW, WCHAR*)
CB1(SetVolumeLabelW, const WCHAR*)
CB1(GetVolumeId, DWORD*)
//CB6(CreateFileW, const WCHAR*, ACCESS_MASK, DWORD, DWORD, CbFsFileInfo*, CbFsHandleInfo*)
//CB6(OpenFileW, const WCHAR*, ACCESS_MASK, DWORD, DWORD, CbFsFileInfo*, CbFsHandleInfo*)
//CB2(CloseFile, CbFsFileInfo*, CbFsHandleInfo*)
CBPROTO(CreateFileW,const WCHAR*, ACCESS_MASK, DWORD, DWORD),
const WCHAR* a1, ACCESS_MASK a2, DWORD a3, DWORD a4, CbFsFileInfo* a5, CbFsHandleInfo* a6) {
  CBSTART(a5);
  keybase_cgo_Cbfs_CreateFileW(&w, a1, a2, a3, a4);
  CBEND;
  a5->set_UserContext((void*)w.file);
}
CBPROTO(OpenFileW,const WCHAR*, ACCESS_MASK, DWORD, DWORD),
const WCHAR* a1, ACCESS_MASK a2, DWORD a3, DWORD a4, CbFsFileInfo* a5, CbFsHandleInfo* a6) {
  CBSTART(a5);
  keybase_cgo_Cbfs_OpenFileW(&w, a1, a2, a3, a4);
  CBEND;
  a5->set_UserContext((void*)w.file);
}
CBPROTO(CloseFile), CbFsFileInfo* a1, CbFsHandleInfo* a2) {
  CBSTART(a1);
  keybase_cgo_Cbfs_CloseFile(&w);
  CBEND;
}


CB15(GetFileInfoW,const WCHAR*,LPBOOL,PFILETIME,PFILETIME,PFILETIME,PFILETIME,
__int64*,__int64*,__int64*,PDWORD,PDWORD,WCHAR*,PWORD,WCHAR*,PWORD);

//CB19(EnumerateDirectoryW,
//CbFsFileInfo*,CbFsHandleInfo*,CbFsDirectoryEnumerationInfo*,
//LPCWSTR,INT,BOOL,LPBOOL,WCHAR*,DWORD*,WCHAR*,UCHAR*,
//FILETIME*,FILETIME*,FILETIME*,FILETIME*,
//__int64*,__int64*,__int64*,DWORD*)

CBPROTO(EnumerateDirectoryW,
LPCWSTR,INT,LPBOOL,WCHAR*,DWORD*,WCHAR*,UCHAR*,
FILETIME*,FILETIME*,FILETIME*,FILETIME*,
__int64*,__int64*,__int64*,DWORD*),
CbFsFileInfo* a1,CbFsHandleInfo* a2,CbFsDirectoryEnumerationInfo* a3,
LPCWSTR a4,INT a5,BOOL a6,LPBOOL a7,WCHAR* a8,DWORD* a9,WCHAR* aa,UCHAR* ab,
FILETIME* ac,FILETIME* ad,FILETIME* ae,FILETIME* af,
__int64* ag,__int64* ah,__int64* ai,DWORD* aj) {
  CBSTART(a1);
  if(!a5)
    a5 = (int)a3->get_UserContext();
  if(a6)
    a5 = 0;
  keybase_cgo_Cbfs_EnumerateDirectoryW(&w,
  a4,a5,a7,a8,a9,aa,ab,ac,ad,ae,af,ag,ah,ai,aj);
  CBEND;
  a3->set_UserContext((void*)(a5+1));
}

//CB2(CloseDirectoryEnumeration, CbFsFileInfo*, CbFsDirectoryEnumerationInfo*)
CBPROTO(CloseDirectoryEnumeration), CbFsFileInfo* a1, CbFsDirectoryEnumerationInfo* a2) {
  CBSTART(a1);
//  keybase_cgo_Cbfs_CloseDirectoryEnumeration(&w);
  CBEND;
}

//CB2(SetAllocationSize, CbFsFileInfo*, __int64)
//CB2(SetEndOfFile, CbFsFileInfo*, __int64)

CBPROTO(SetAllocationSize,__int64), CbFsFileInfo* a1, __int64 a2) {
  CBSTART(a1);
  keybase_cgo_Cbfs_SetAllocationSize(&w, a2);
  CBEND;
}
CBPROTO(SetEndOfFile,__int64), CbFsFileInfo* a1, __int64 a2) {
  CBSTART(a1);
  keybase_cgo_Cbfs_SetEndOfFile(&w, a2);
  CBEND;
}

CBPROTO(SetFileAttributesW,FILETIME*,FILETIME*,FILETIME*,FILETIME*,DWORD),
CbFsFileInfo* a1,CbFsHandleInfo* a2,
FILETIME* a3,FILETIME* a4,FILETIME* a5,FILETIME* a6, DWORD a7) {
  CBSTART(a1);
  keybase_cgo_Cbfs_SetFileAttributesW(&w, a3,a4,a5,a6,a7);
  CBEND;
}
//CB7(SetFileAttributesW,
//CbFsFileInfo*,CbFsHandleInfo*,
//FILETIME*,FILETIME*,FILETIME*,FILETIME*,
//DWORD)

CBPROTO(CanFileBeDeleted,BOOL*), CbFsFileInfo* a1, CbFsHandleInfo* a2, BOOL *a3) {
  CBSTART(a1);
  keybase_cgo_Cbfs_CanFileBeDeleted(&w, a3);
  CBEND;
}
CBPROTO(DeleteFileW, LPCWSTR), CbFsFileInfo* a1) {
  CBSTART(a1); 
  // Unfortunately the file is already closed here...
  auto t1 = a1->get_FileNameBuffer();
  keybase_cgo_Cbfs_DeleteFileW(&w, t1);
  CBEND;
}
//CB3(CanFileBeDeleted,CbFsFileInfo*,CbFsHandleInfo*,BOOL*)
//CB1(DeleteFileW,CbFsFileInfo*)
//CB2(RenameOrMoveFileW, CbFsFileInfo*,LPCWSTR)
CBPROTO(RenameOrMoveFileW, LPCWSTR,LPCWSTR), CbFsFileInfo* a1, LPCWSTR a2) {
  CBSTART(a1);
  auto t1 = a1->get_FileNameBuffer();
  keybase_cgo_Cbfs_RenameOrMoveFileW(&w, t1, a2);
  CBEND;
}
//CB5(ReadFile, CbFsFileInfo*, __int64, PVOID, DWORD, PDWORD)
//CB5(WriteFile, CbFsFileInfo*, __int64, PVOID, DWORD, PDWORD)
//CB3(IsDirectoryEmpty, CbFsFileInfo*, LPCWSTR, LPBOOL)

CBPROTO(ReadFile, __int64, PVOID, DWORD, PDWORD),
CbFsFileInfo* a1, __int64 a2, PVOID a3, DWORD a4, PDWORD a5) {
  CBSTART(a1);
  keybase_cgo_Cbfs_ReadFile(&w,a2,a3,a4,a5);
  CBEND;
}
CBPROTO(WriteFile, __int64, PVOID, DWORD, PDWORD),
CbFsFileInfo* a1, __int64 a2, PVOID a3, DWORD a4, PDWORD a5) {
  CBSTART(a1);
  keybase_cgo_Cbfs_WriteFile(&w,a2,a3,a4,a5);
  CBEND;
}
CBPROTO(IsDirectoryEmpty,BOOL*), CbFsFileInfo* a1, LPCWSTR a2, BOOL *a3) {
  CBSTART(a1);
  keybase_cgo_Cbfs_IsDirectoryEmpty(&w, a3);
  CBEND;
}




#define EHSTART try {
#define EHDONE } catch(ECBFSError err) { error_wrap(err, w); }

EXT_C int keybase_cbfs_install(keybase_Cbfs_wrap *w, LPCSTR appid, LPCWSTR cabfilename) {
  EHSTART
  DWORD need_reboot = 0;
  CallbackFileSystem::Install(cabfilename,appid,nullptr,
  TRUE, CBFS_MODULE_MOUNT_NOTIFIER_DLL, &need_reboot);
  return need_reboot;
  EHDONE
  return 0;
}

EXT_C void keybase_Cbfs_Initialize(keybase_Cbfs_wrap *w, LPCSTR appid, LPCSTR key) {
  EHSTART
  auto fs = w->fs;

  fs->SetCallAllOpenCloseCallbacks(FALSE);
  fs->SetOnMount(keybase_Cbfs_Mount);
  fs->SetOnUnmount(keybase_Cbfs_Unmount);
  fs->SetOnGetVolumeSize(keybase_Cbfs_GetVolumeSize);
  fs->SetOnGetVolumeLabel(keybase_Cbfs_GetVolumeLabelW);
  fs->SetOnSetVolumeLabel(keybase_Cbfs_SetVolumeLabelW);
  fs->SetOnGetVolumeId(keybase_Cbfs_GetVolumeId);
  fs->SetOnCreateFile(keybase_Cbfs_CreateFileW);
  fs->SetOnOpenFile(keybase_Cbfs_OpenFileW);
  fs->SetOnCloseFile(keybase_Cbfs_CloseFile);
  fs->SetOnGetFileInfo(keybase_Cbfs_GetFileInfoW);
  fs->SetOnEnumerateDirectory(keybase_Cbfs_EnumerateDirectoryW);
  fs->SetOnCloseDirectoryEnumeration(keybase_Cbfs_CloseDirectoryEnumeration);
  fs->SetOnSetAllocationSize(keybase_Cbfs_SetAllocationSize);
  fs->SetOnSetEndOfFile(keybase_Cbfs_SetEndOfFile);
  fs->SetOnSetFileAttributes(keybase_Cbfs_SetFileAttributesW);
  fs->SetOnCanFileBeDeleted(keybase_Cbfs_CanFileBeDeleted);
  fs->SetOnDeleteFile(keybase_Cbfs_DeleteFileW);
  fs->SetOnRenameOrMoveFile(keybase_Cbfs_RenameOrMoveFileW);
  fs->SetOnReadFile(keybase_Cbfs_ReadFile);
  fs->SetOnWriteFile(keybase_Cbfs_WriteFile);
  fs->SetOnIsDirectoryEmpty(keybase_Cbfs_IsDirectoryEmpty);

  if(key) {
    fs->SetRegistrationKey(key);
  }
    fs->Initialize(appid);

  fs->CreateStorage();

  /*
  fs->SetNonexistentFilesCacheEnabled(FALSE);
  fs->SetMetaDataCacheEnabled(FALSE);
  fs->SetFileCacheEnabled(FALSE);
  */

  EHDONE
}

EXT_C void keybase_cbfs_getmodulestatus(const char * ProductName, INT Module, BOOL *Installed, INT *FileVersionHigh, INT *FileVersionLow, SERVICE_STATUS *ServiceStatus) {
  CallbackFileSystem::GetModuleStatus(ProductName, Module, Installed, FileVersionHigh, FileVersionLow, ServiceStatus);
}

EXT_C void keybase_cbfs_mountmedia(keybase_Cbfs_wrap *w, int timeout_in_milliseconds) {
  EHSTART
  w->fs->MountMedia(timeout_in_milliseconds); 
  EHDONE
}
EXT_C void keybase_cbfs_addmountingpoint(keybase_Cbfs_wrap *w, LPCWSTR mp, DWORD flags) {
  EHSTART
  w->fs->AddMountingPoint(mp, flags, nullptr);
  EHDONE
}

EXT_C void keybase_cbfs_deletestorage(keybase_Cbfs_wrap *w, int force) {
  EHSTART
  w->fs->DeleteStorage(force);
  EHDONE
}

#endif /* Windows check */
