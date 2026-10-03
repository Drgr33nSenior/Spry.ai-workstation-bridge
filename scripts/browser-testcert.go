// browser-testcert creates an ephemeral, hostname-bound certificate for the
// isolated browser regression. It is not an installation or certificate tool.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"math/big"
	"os"
	"strings"
	"time"
)

func main() {
	host := flag.String("host", "", "test DNS hostname")
	certPath := flag.String("cert", "", "certificate output")
	keyPath := flag.String("key", "", "private-key output")
	flag.Parse()
	hosts := strings.Split(*host, ",")
	if *host == "" || *certPath == "" || *keyPath == "" || hosts[0] == "" {
		fatal(errors.New("host, cert and key are required"))
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		fatal(err)
	}
	now := time.Now()
	for _, name := range hosts {
		if name == "" {
			fatal(errors.New("test DNS hostname is empty"))
		}
	}
	template := x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: hosts[0]}, DNSNames: hosts, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		fatal(err)
	}
	if err = os.WriteFile(*certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		fatal(err)
	}
	if err = os.WriteFile(*keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_, _ = os.Stderr.WriteString(err.Error() + "\n")
	os.Exit(1)
}
