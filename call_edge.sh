#!/usr/bin/env bash

# check if 3 arguments are given
if [ $# -ne 3 ]; then
    echo "Usage: $0 <func> <target> <N>"
    exit 1
fi

# read arguments
FUNC=$1
TARGET=$2
N=$3

for i in $(seq 1 $N); do
    xh :80$TARGET/$FUNC &
done
