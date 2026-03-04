#!/bin/bash
# Wrapper for MongoDB MCP server.
# Starts kubectl port-forward before launching the MCP server,
# and cleans up when the MCP server exits.

PORT=27017
NAMESPACE=devops-platform
SERVICE=mongo

# Start port-forward in background
kubectl port-forward -n "$NAMESPACE" svc/"$SERVICE" "$PORT:$PORT" &
PF_PID=$!

# Ensure port-forward is killed when this script exits
trap "kill $PF_PID 2>/dev/null" EXIT

# Wait for port to be ready (up to 10s)
for i in $(seq 1 10); do
  if nc -z localhost $PORT 2>/dev/null; then
    break
  fi
  sleep 1
done

# Start MCP server
exec npx -y mcp-mongo-server "mongodb://localhost:$PORT/devops_platform"
