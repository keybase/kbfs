// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

#ifndef KBFS_CBFS_BINDING_H__
#define KBFS_CBFS_BINDING_H__

#if defined(_WIN32) || defined(WIN32) || defined(__CYGWIN__) || defined(__MINGW32__) || defined(__BORLANDC__)

#define UNICODE 1
#define _UNICODE 1
#define _REENTRANT 1
#define _THREAD_SAFE 1
#define _FILE_OFFSET_BITS 64
#define WIN32_LEAN_AND_MEAN	1

#include <stdint.h>
#include <stdlib.h>
#include <windows.h>
#include <ntdef.h>
#include <ntstatus.h>

#ifdef HAVE_CBFS
#include <CbFS.h>
#define EXT_C extern "C"
#else
typedef struct CallbackFileSystem {} CallbackFileSystem;
typedef struct CbFsFileInfo {} CbFsFileInfo;
typedef struct CbFsHandleInfo {} CbFsHandleInfo;
typedef struct CbFsDirectoryEnumerationInfo {} CbFsDirectoryEnumerationInfo;
#define EXT_C
#endif /* HAVE_CBFS */

typedef struct keybase_Cbfs_wrap {
  // This is a non-owning pointer.
  CallbackFileSystem *fs;
  DWORD ecode;
  // This pointer owns the emsg.
  LPCWSTR emsg;
  uint32_t tag;
  uint32_t file;
} keybase_Cbfs_wrap;

typedef struct keybase_Cbfs_fhw {
  keybase_Cbfs_wrap w;
  CbFsFileInfo* FileInfo;
  CbFsHandleInfo* HandleInfo;
} keybase_Cbfs_fhw;

EXT_C CallbackFileSystem* keybase_Cbfs_New_CallbackFileSystem(uint32_t tag, uint32_t *max_filename_length_ptr);
EXT_C void keybase_Cbfs_Delete_CallbackFileSystem(CallbackFileSystem* fs);
EXT_C int keybase_cbfs_install(keybase_Cbfs_wrap *w, LPCSTR appid, LPCWSTR cabfilename);
EXT_C void keybase_Cbfs_Initialize(keybase_Cbfs_wrap *w, LPCSTR appid, LPCSTR key);
EXT_C void keybase_cbfs_getmodulestatus(const char * ProductName, INT Module, BOOL *Installed, INT *FileVersionHigh, INT *FileVersionLow, SERVICE_STATUS *ServiceStatus);
EXT_C void keybase_cbfs_mountmedia(keybase_Cbfs_wrap *w, int timeout_in_milliseconds);
EXT_C void keybase_cbfs_addmountingpoint(keybase_Cbfs_wrap *w, LPCWSTR mp, DWORD flags);
EXT_C void keybase_cbfs_deletestorage(keybase_Cbfs_wrap *w, int force);

#endif /* windows check */

#endif /* KBFS_CBFS_BINDING_H__ */
