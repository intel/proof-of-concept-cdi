#!/bin/sh

# Directory to use for storing intermediate files.
CA=${CA:="cdi-ca"}
WORKDIR=${WORKDIR:-$(mktemp -d -u -t cdi-XXXX)}
mkdir -p $WORKDIR
cd $WORKDIR

# Check for cfssl utilities.
cfssl_found=1
(command -v cfssl 2>&1 >/dev/null && command -v cfssljson 2>&1 >/dev/null) || cfssl_found=0
if [ $cfssl_found -eq 0 ]; then
    echo "cfssl tools not found, Please install cfssl and cfssljson."
    exit 1
fi

# Generate CA certificates.
<<EOF cfssl gencert -initca - | cfssljson -bare ca
{
    "CN": "$CA",
    "key": {
        "algo": "rsa",
        "size": 2048
    }
}
EOF

# Generate server and client certificates.
DEFAULT_CNS="cdi-registry cdi-node-controller"
CNS="${DEFAULT_CNS} ${EXTRA_CNS:=""}"
for name in ${CNS}; do
  <<EOF cfssl gencert -ca=ca.pem -ca-key=ca-key.pem - | cfssljson -bare $name
{
    "CN": "$name",
    "hosts": [
        "$name"
    ],
    "key": {
        "algo": "ecdsa",
        "size": 256
    }
}
EOF
done
