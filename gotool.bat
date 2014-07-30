@echo off
:: Copyright 2012 Google Inc. All rights reserved.
:: Use of this source code is governed by the Apache 2.0
:: license that can be found in the LICENSE file.
setlocal
set GOROOT=%~dp0\goroot
set APPENGINE_DEV_APPSERVER=%~dp0\dev_appserver.py
set GOARCH=
set GOBIN=
set GOOS=

:: Note that due to the nature of BAT files, if the optional
:: "--dev_appserver Z:\path\to\dev_appserver.py" arguments are
:: provided, they must appear exactly as the first and second
:: arguments after the command name for this to work properly.
:: Also note that shifting args messes up the value of %~n0.
set EXENAME=%~n0.exe
if "%1"=="--dev_appserver" (
    set APPENGINE_DEV_APPSERVER=%2
    shift & shift
)

:: Set a GOPATH if one is not set.
if not "%GOPATH%"=="" goto havepath
set GOPATH=%~dp0\gopath
:havepath

:: Note that %* can not be used with shift.
%GOROOT%\bin\%EXENAME% %1 %2 %3 %4 %5 %6 %7 %8 %9
