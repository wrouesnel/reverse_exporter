#!/bin/bash

cat << EOF
# HELP dynamic_script_metric This is a sample metric generated from a script
# TYPE dynamic_script_metric gauge
dynamic_script_metric $(date +%s)
EOF
