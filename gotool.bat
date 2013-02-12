@echo off
:: Copyright 2012 Google Inc. All rights reserved.
:: Use of this source code is governed by the Apache 2.0
:: license that can be found in the LICENSE file.
setlocal
set GOROOT=%~dp0\goroot
set GOBIN=

:: Set a GOPATH if one is not set.
if not "%GOPATH%"=="" goto havepath
set GOPATH=%~dp0\gopath
:havepath

%GOROOT%\bin\%~n0.exe %*
