#!/bin/sh
set -eu

binary=${1:-./bin/test-inbox-mcp}

output=$(
  printf '%s\n' \
    '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke-test","version":"1"}}}' \
    '{"jsonrpc":"2.0","method":"notifications/initialized"}' \
    '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
    '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"new_address","arguments":{"local_part":"offline-smoke"}}}' \
    | INBOX_DOMAIN=smoke.invalid "$binary"
)

line_count=$(printf '%s\n' "$output" | wc -l | tr -d ' ')
[ "$line_count" = "3" ]
printf '%s\n' "$output" | grep -q '"protocolVersion":"2024-11-05"'
printf '%s\n' "$output" | grep -q '"name":"new_address"'
printf '%s\n' "$output" | grep -q 'offline-smoke@smoke.invalid'

printf '%s\n' "stdio smoke test passed"
