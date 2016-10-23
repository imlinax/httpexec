//usr/bin/go run $0 $@ ; exit
// httpexec in Go. Copyright (C) Kost. Distributed under MIT.
// RESTful interface to your operating system shell

package main

import (
	"flag"
	"log"
	"io/ioutil"
	"crypto/tls"
	"crypto/x509"
	"net/url"
	"net/http"
	"net/http/cgi"
	"encoding/base64"
	"encoding/json"
	"strings"
	"bytes"
	"os/exec"
)

// JSON input request
type CmdReq struct{
	Cmd	string
	Nojson	bool
	Stdin	string
}

// JSON output request
type CmdResp struct{
	Cmd	string
	Stdout	string
	Stderr	string
	Err	string
}

var auth string		// basic authentication combo
var realm string	// basic authentication realm
var VerboseLevel int	// global verbosity level
var SilentOutput bool	// silent output

// check basic authentication if set
func checkAuth(w http.ResponseWriter, r *http.Request) bool {
	s := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(s) != 2 { return false }

	b, err := base64.StdEncoding.DecodeString(s[1])
	if err != nil { return false }

	return bytes.Equal(b,[]byte(auth))
}

// real content Handler
func contHandler(w http.ResponseWriter, r *http.Request) {
	var jsonout bool
	var inputjson CmdReq
	var outputjson CmdResp
	var body []byte
	if (r.Header.Get("Content-Type") == "application/json") {
		w.Header().Set("Content-Type", "application/json")
		jsonout = true
	} else {
		w.Header().Set("Content-Type", "text/plain")
	}
	cmdstr:=""
	urlq,_:=url.QueryUnescape(r.URL.RawQuery)
	if (r.Method == "GET" || r.Method == "HEAD") {
		cmdstr=urlq
	}
	if (r.Method == "POST") {
		var rerr error
		body, rerr = ioutil.ReadAll(r.Body)
		if rerr != nil {
		}
		if (VerboseLevel>2) { log.Printf("Body: %s", body) }

		if (len(urlq)>0 && r.Method == "POST") {
			cmdstr=urlq
		} else {
			if (jsonout) {
				jerr := json.Unmarshal(body,&inputjson)
				if jerr != nil {
				    // http.Error(w, jerr.Error(), 400)
				    return
				}
				cmdstr=inputjson.Cmd
				jsonout=!inputjson.Nojson
			} else {
				cmdstr=string(body)
			}
		}
	}
	if (VerboseLevel>0) { log.Printf("Command to execute: %s", cmdstr) }

	if len(cmdstr)<1 {
		return
	}

	parts := strings.Fields(cmdstr)
	head := parts[0]
	parts = parts[1:len(parts)]

	cmd := exec.Command(head, parts...)

	// Handle stdin if have any
	if (len(urlq)>0 && r.Method == "POST") {
		if (VerboseLevel>2) { log.Printf("Stdin: %s", body) }
		cmd.Stdin = bytes.NewReader(body)
	}
	if (len(inputjson.Stdin)>0) {
		if (VerboseLevel>2) { log.Printf("JSON Stdin: %s", inputjson.Stdin) }
		cmd.Stdin = strings.NewReader(inputjson.Stdin)
	}

	var err error
	var jStdout bytes.Buffer
	var jStderr bytes.Buffer
	if (r.Method == "HEAD") {
		err = cmd.Start()
	} else {
		if (jsonout) {
			cmd.Stdout = &jStdout
			cmd.Stderr = &jStderr
		} else {
			cmd.Stdout = w
			cmd.Stderr = w
		}
		err = cmd.Run()
	}
	if err != nil {
		if (VerboseLevel>0) { log.Printf("Error executing: %s", err) }
		if (jsonout) {
			outputjson.Err=err.Error()
		} else {
			if (!SilentOutput) { w.Write([]byte(err.Error())) }
		}
	}

	if (jsonout) {
		outputjson.Stdout=jStdout.String()
		outputjson.Stderr=jStderr.String()
		outputjson.Cmd=cmdstr
		json.NewEncoder(w).Encode(outputjson)
	}
}

func retlogstr(entry string) (string) {
	if (len(entry)>0) {
		return entry
	} else {
		return "-"
	}
}

// main handler which basically checks (basic) authentication first
func handler(w http.ResponseWriter, r *http.Request) {
	if (VerboseLevel>0) {
		log.Printf("%s %s %s %s %s", retlogstr(r.RemoteAddr), retlogstr(r.Header.Get("X-Forwarded-For")), r.Method, r.RequestURI, retlogstr(r.URL.RawQuery))
	}
	if (auth == "") {
		contHandler(w, r)
	} else {
		if checkAuth(w, r) {
			contHandler(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="`+realm+`"`)
		w.WriteHeader(401)
		w.Write([]byte("401 Unauthorized\n"))
	}
}

// main function with main http loop and command line parsing
func main() {
	flag.StringVar(&auth, "auth", "", "basic auth to require - in form user:pass")
	optcgi := flag.Bool("cgi", false, "CGI mode")
	cert := flag.String("cert", "server.crt", "SSL/TLS certificate file")
	key := flag.String("key", "server.key", "SSL/TLS certificate key file")
	uri := flag.String("uri", "/", "URI to serve")
	listen := flag.String("listen", ":8080", "listen address and port")
	flag.StringVar(&realm, "realm", "httpexec", "Basic authentication realm")
	opttls := flag.Bool("tls",false,"use TLS/SSL")
	optssl := flag.Bool("ssl",false,"use TLS/SSL")
	optverify := flag.String("verify", "", "Client cert verification using SSL/TLS (CA) certificate file")
	flag.BoolVar(&SilentOutput, "silentout", false, "Silent Output (do not display errors)")
	flag.IntVar(&VerboseLevel, "verbose", 0, "verbose level")

	flag.Parse()

	if (VerboseLevel>0) { log.Printf("Starting to listen at %s with URI %s with auth %s",*listen,*uri, auth) }

	if (*optcgi) {
		cgi.Serve(http.HandlerFunc(handler))
	} else {
		http.HandleFunc(*uri, handler)
		var err error
		if (*opttls || *optssl) {
			// secure default TLS configuration
			tlsCfg := &tls.Config{
				MinVersion:	tls.VersionTLS12,
				CurvePreferences: []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
				PreferServerCipherSuites: true,
				CipherSuites: []uint16{
					tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
					tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
					tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
					tls.TLS_RSA_WITH_AES_256_CBC_SHA,
				},
			}
			// if verify is specified add only specific (CA) certificate to cert pool
			if (len(*optverify)>0) {
				caCertPool := x509.NewCertPool()
				caCert, err := ioutil.ReadFile(*optverify)
				if err != nil {
					log.Fatal("Error reading client verification cert: ", err)
				}
				caCertPool.AppendCertsFromPEM(caCert)

				tlsCfg.ClientCAs = caCertPool
				tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
				tlsCfg.BuildNameToCertificate()
			}

			srv := &http.Server{
				Addr:		*listen,
				TLSConfig:	tlsCfg,
			}
			err = srv.ListenAndServeTLS(*cert, *key)
		} else {
			err = http.ListenAndServe(*listen, nil)
		}
		if err != nil {
			log.Fatal("ListenAndServe: ", err)
		}
	}
}

