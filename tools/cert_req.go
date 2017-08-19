package tools

import (
	"bufio"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	allTools = append(allTools, certReqTools{})
}

type certReqTools struct{}

func (certReqTools) Name() string {
	return "cert_req"
}

func (certReqTools) Description() string {
	return "Generate an x509 CSR for requesting a client certificate"
}

func (c certReqTools) Run(args []string) {
	fs := flag.NewFlagSet("cert_req", flag.ExitOnError)
	out := fs.String("out", ".", "directory to store the output files")
	bits := fs.Int("bits", 2048, "number of bits of the generated RSA keypair")
	fs.Parse(args)
	if *out == "" {
		fs.Usage()
	} else if s, err := os.Stat(*out); err != nil {
		panic(err)
	} else if !s.IsDir() {
		panic(*out + " is not a valid directory")
	}

	csr := c.getCertInfo()
	privKey, err := rsa.GenerateKey(rand.Reader, *bits)
	if err != nil {
		panic(err)
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csr, privKey)
	if err != nil {
		panic(err)
	}

	c.saveAsPEM("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(privKey),
		filepath.Join(*out, "cert.key.pem"), 0600)
	c.saveAsPEM("CERTIFICATE REQUEST", csrDER,
		filepath.Join(*out, "cert.csr"), 0644)
}

func (certReqTools) getCertInfo() *x509.CertificateRequest {
	csr := &x509.CertificateRequest{SignatureAlgorithm: x509.SHA256WithRSA}
	r := bufio.NewReader(os.Stdin)

	_, _ = fmt.Print("Country (e.g. CN): ")
	if s, err := r.ReadString('\n'); err != nil {
		panic(err)
	} else {
		csr.Subject.Country = []string{strings.TrimSpace(s)}
	}

	_, _ = fmt.Print("Common name: ")
	if s, err := r.ReadString('\n'); err != nil {
		panic(err)
	} else {
		csr.Subject.CommonName = strings.TrimSpace(s)
	}

	_, _ = fmt.Print("Email address: ")
	if s, err := r.ReadString('\n'); err != nil {
		panic(err)
	} else {
		csr.EmailAddresses = []string{strings.TrimSpace(s)}
	}

	return csr
}

func (certReqTools) saveAsPEM(
	pemType string, der []byte, dstPath string, perm uint16) {
	f, err := os.OpenFile(dstPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL, os.ModePerm&os.FileMode(perm))
	if err != nil {
		panic(err)
	}
	defer f.Close() // nolint: errcheck
	pemBlock := &pem.Block{Type: pemType, Bytes: der}
	if err = pem.Encode(f, pemBlock); err != nil {
		panic(err)
	}
}
