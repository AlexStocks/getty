#!/usr/bin/env bash

cd echo/tcp-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/tcp-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/udp-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/udp-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/ws-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/ws-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -

cd echo/wss-echo/client && sh assembly/mac/dev.sh && rm -rf target && cd -
cd echo/wss-echo/server && sh assembly/mac/dev.sh && rm -rf target && cd -
