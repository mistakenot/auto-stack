#!/bin/sh
set -ex

CERT_DIR="/certs"
mkdir -p "$CERT_DIR"

# Generate CA key + self-signed cert
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
    -days 3650 -nodes \
    -keyout "$CERT_DIR/ca.key" \
    -out "$CERT_DIR/ca.crt" \
    -subj "/CN=Harness CA"

# Generate server key + CSR
openssl req -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
    -nodes \
    -keyout "$CERT_DIR/server.key" \
    -out "$CERT_DIR/server.csr" \
    -subj "/CN=git-server"

# Sign server cert with CA, adding SAN
printf "subjectAltName=DNS:git-server\n" > /tmp/ext.cnf
openssl x509 -req -in "$CERT_DIR/server.csr" \
    -CA "$CERT_DIR/ca.crt" -CAkey "$CERT_DIR/ca.key" \
    -CAcreateserial -days 3650 \
    -extfile /tmp/ext.cnf \
    -out "$CERT_DIR/server.crt"

rm -f "$CERT_DIR/server.csr" "$CERT_DIR/ca.srl" /tmp/ext.cnf
chmod 644 "$CERT_DIR/ca.crt"
echo "Certs generated in $CERT_DIR"
