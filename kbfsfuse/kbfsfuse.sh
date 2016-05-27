#!/bin/bash

_term() {
    echo "Caught TERM signal"
    kill -TERM "$SERVICE"
    kill -TERM "$KBFS"
    exit 0
}
trap _term SIGTERM

keybase service &
SERVICE=$!
kbfsfuse -debug -mdserver mdserver.kbfs.local:8125 -bserver bserver.kbfs.local:8225 -enable-sharing-before-signup /keybase &
KBFS=$!

wait "$SERVICE"
wait "$KBFS"
