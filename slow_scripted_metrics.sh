#!/bin/bash

echo "This is a slow endpoint" 1>&2
sleep 3

cat << EOF
# HELP slow_dynamic_script_metric This is a sample metric generated from a script which is slow and needs caching
# TYPE slow_dynamic_script_metric gauge
slow_dynamic_script_metric $(date +%s)
EOF
