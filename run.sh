#!/usr/bin/env bash

cd erigon
exec ./build/bin/erigon --prune.mode=minimal
