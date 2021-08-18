#!/usr/bin/env bash
# ******************************************************
# DESC    :
# AUTHOR  : Alex Stocks
# VERSION : 1.0
# LICENCE : Apache License 2.0
# EMAIL   : alexstocks@foxmail.com
# MOD     : 2019-09-07 18:32
# FILE    : build_all.sh
# ******************************************************


cd echo/tcp-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/tcp-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/udp-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/udp-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/ws-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/ws-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/wss-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/wss-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd rpc/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd rpc/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd micro/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd micro/server && sh assembly/mac/dev.sh && rm -rf target && cd -

