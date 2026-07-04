#!/bin/sh
# Wait for the CA cert from git-server, then configure git to trust it.
echo "Waiting for CA cert..."
while [ ! -f /shared-certs/ca.crt ]; do sleep 0.2; done
git config --global http.sslCAInfo /shared-certs/ca.crt
echo "CA cert loaded."
exec "$@"
