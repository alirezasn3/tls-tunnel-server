package main

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"sync"
)

var configs []ServerConfig

type ServerConfig struct {
	ServiceName         string `json:"serviceName"`
	Connect             string `json:"connect"`
	Listen              string `json:"listen"`
	Protocol            string `json:"protocol"`
	CertificateLocation string `json:"certificateLocation"`
	KeyLocation         string `json:"KeyLocation"`
	TLSConfig           tls.Config
}

func handleError(err error, fatal bool, config *ServerConfig) bool {
	if err != nil {
		if fatal {
			log.Fatalln("["+config.ServiceName+"]"+" [error] ", err)
		} else {
			log.Println("["+config.ServiceName+"]"+" [error] ", err)
		}
		return true
	}
	return false
}

func logMessage(message string, config *ServerConfig) {
	log.Println("[" + config.ServiceName + "] " + "[info] " + message)
}

func loadConfigFile(configs *[]ServerConfig) {
	bytes, err := os.ReadFile("config.json")
	if err != nil {
		log.Fatalln("[error] ", err)
	}
	err = json.Unmarshal(bytes, &configs)
	if err != nil {
		log.Fatalln("[error] ", err)
	}
	log.Println("[info] config file loaded")
}

func loadCertificates(config *[]ServerConfig) {
	for i := range configs {
		certificate, err := tls.LoadX509KeyPair(configs[i].CertificateLocation, configs[i].KeyLocation)
		if err != nil {
			log.Fatalln("[error] ", err)
		}
		configs[i].TLSConfig.MinVersion = tls.VersionTLS12
		configs[i].TLSConfig.CurvePreferences = []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256}
		configs[i].TLSConfig.PreferServerCipherSuites = true
		configs[i].TLSConfig.CipherSuites = []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		}
		configs[i].TLSConfig.Certificates = []tls.Certificate{certificate}
		logMessage("certificates loaded", &configs[i])
	}
}
func handleRemoteClient(remoteConnection net.Conn, config ServerConfig) {
	defer remoteConnection.Close()

	if config.Protocol == "tcp" {
		// create connection to the local app
		localConnection, err := net.Dial("tcp", config.Connect)
		if handleError(err, false, &config) {
			logMessage("remote connection from "+remoteConnection.RemoteAddr().String()+" closed because could not create connection to the local app on "+config.Connect, &config)
			return
		}
		defer localConnection.Close()
		logMessage(remoteConnection.RemoteAddr().String()+" connected to "+config.Connect, &config)

		// listen for incoming traffic from remote machine and forward it to local app
		go func() {
			writtenBytes, _ := io.Copy(localConnection, remoteConnection)
			logMessage("wrote "+strconv.Itoa(int(writtenBytes))+" bytes to "+localConnection.RemoteAddr().String(), &config)
		}()

		// listen for incoming traffic from local app and forward it to remote machine
		writtenBytes, _ := io.Copy(remoteConnection, localConnection)
		logMessage("wrote "+strconv.Itoa(int(writtenBytes))+" bytes to "+remoteConnection.RemoteAddr().String(), &config)
	} else {
		// create udp address objects from connect and listen addresses
		listenAddress, err := net.ResolveUDPAddr("udp", ":0")
		if handleError(err, false, &config) {
			return
		}
		connectAddress, err := net.ResolveUDPAddr("udp", config.Connect)
		if handleError(err, false, &config) {
			return
		}

		// creat udp connection to local app
		localUDPConnection, err := net.ListenUDP("udp", listenAddress)
		if handleError(err, false, &config) {
			return
		}
		logMessage(remoteConnection.RemoteAddr().String()+" connected to "+localUDPConnection.LocalAddr().String()+" then to "+config.Connect, &config)

		// listen for incoming traffic from remote machine and forward it to local app
		go func() {
			// defer wg.Done()
			buff := make([]byte, 1024*16)
			for {
				readBytes, err := remoteConnection.Read(buff)
				if handleError(err, false, &config) {
					remoteConnection.Close()
					localUDPConnection.Close()
					break
				}
				localUDPConnection.WriteToUDP(buff[:readBytes], connectAddress) // TODO: handle error
			}
		}()

		// listen for incoming traffic from local app and forward it to remote machine
		io.Copy(remoteConnection, localUDPConnection)
	}

	logMessage("remote connection "+remoteConnection.RemoteAddr().String()+" closed", &config)
}

func main() {
	// load server config and certificates
	loadConfigFile(&configs)
	loadCertificates(&configs)

	// add wait group to manage go routines
	var wg sync.WaitGroup

	for _, config := range configs {
		wg.Add(1)
		go func(config ServerConfig) {
			defer wg.Done()

			// create tcp listener on local machine
			localListener, err := tls.Listen("tcp", config.Listen, &config.TLSConfig)
			if err != nil {
				log.Fatalln("[error] ", err)
			}
			logMessage("listening on "+config.Listen, &config)

			// accept new connections from remote machine
			for {
				remoteConnection, err := localListener.Accept()
				if handleError(err, false, &config) {
					continue
				}
				logMessage("accepted connection from "+remoteConnection.RemoteAddr().String(), &config)
				// handle each new client on seperate go routine
				go handleRemoteClient(remoteConnection, config)
			}
		}(config)
	}

	wg.Wait()
}
