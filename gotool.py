#!/usr/bin/env python
#
# Copyright 2011 Google Inc. All rights reserved.
# Use of this source code is governed by the Apache 2.0
# license that can be found in the LICENSE file.

"""Convenience wrapper for starting a Go tool in the App Engine SDK."""

import os
import sys

SDK_BASE = os.path.abspath(os.path.dirname(os.path.realpath(__file__)))
GOROOT = os.path.join(SDK_BASE, 'goroot')

if __name__ == '__main__':
  tool = os.path.basename(__file__)
  bin = os.path.join(GOROOT, 'bin', tool)
  os.environ['GOROOT'] = GOROOT
  for key in ('GOBIN', 'GOPATH'):
    if key in os.environ:
      del os.environ[key]
  os.execve(bin, sys.argv, os.environ)
