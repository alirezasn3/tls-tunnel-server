package main

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net"
	"os"
	"sync"
)

var config ServerConfig

type ServerConfig struct {
	Connect             string `json:"connect"`
	Listen              string `json:"listen"`
	Protocol            string `json:"protocol"`
	CertificateLocation string `json:"certificateLocation"`
	KeyLocation         string `json:"KeyLocation"`
	TLSConfig           tls.Config
}

func handleError(err error, fatal bool) bool {
	if err != nil {
		if fatal {
			log.Fatalln("[error] ", err)
		} else {
			log.Println("[error] ", err)
		}
		return true
	}
	return false
}

func logMessage(message string) {
	log.Println("[info] " + message)
}

func loadConfigFile(config *ServerConfig) {
	bytes, err := os.ReadFile("config.json")
	handleError(err, true)
	err = json.Unmarshal(bytes, &config)
	handleError(err, true)
	logMessage("config file loaded")
}

func loadCertificates(config *ServerConfig) {
	certificate, err := tls.LoadX509KeyPair(config.CertificateLocation, config.KeyLocation)
	handleError(err, true)
	config.TLSConfig.Certificates = []tls.Certificate{certificate}
	logMessage("certificates loaded")
}

func handleRemoteClient(remoteConnection net.Conn, err error) {
	defer logMessage("remote connection " + remoteConnection.RemoteAddr().String() + " closed")
	defer remoteConnection.Close()

	// check if connection was successfull else exit go routine
	if handleError(err, false) {
		return
	}
	logMessage("accepted connection from " + remoteConnection.RemoteAddr().String())

	if config.Protocol == "tcp" {
		// create connection to the local app
		localConnection, err := net.Dial("tcp", config.Connect)
		if handleError(err, false) {
			logMessage("remote connection from " + remoteConnection.RemoteAddr().String() + " closed because could not create connection to the local app on " + config.Connect)
			return
		}
		defer localConnection.Close()
		logMessage(remoteConnection.RemoteAddr().String() + " connected to " + config.Connect)

		// create wait group for go routines
		var wg sync.WaitGroup

		// listen for incoming traffic from remote machine and forward it to local app
		wg.Add(1)
		go func() {
			defer wg.Done()

			buff := make([]byte, 1024*16)

			for {
				readBytes, _ := remoteConnection.Read(buff) // TODO: check for error
				localConnection.Write(buff[:readBytes])     // TODO: check for error
			}
		}()

		// listen for incoming traffic from local app and forward it to remote machine
		wg.Add(1)
		go func() {
			defer wg.Done()

			buff := make([]byte, 1024*16)

			for {
				readBytes, _ := localConnection.Read(buff) // TODO: check for error
				remoteConnection.Write(buff[:readBytes])   // TODO: check for error
			}
		}()

		// wait for go routines to finish
		wg.Wait()
	} else {
		// creat udp connection to local app
		listenAddress, err := net.ResolveUDPAddr("udp", ":0")
		if handleError(err, false) {
			return
		}
		localUDPConnection, err := net.ListenUDP("udp", listenAddress)
		if handleError(err, false) {
			return
		}
		defer localUDPConnection.Close()
		logMessage(remoteConnection.RemoteAddr().String() + " connected to " + localUDPConnection.LocalAddr().String() + " then to " + config.Connect)

		// create wait group for go routines
		var wg sync.WaitGroup

		// listen for incoming traffic from remote machine and forward it to local app
		wg.Add(1)
		go func() {
			defer wg.Done()

			connectAddress, err := net.ResolveUDPAddr("udp", config.Connect)
			if handleError(err, false) {
				return
			}

			buff := make([]byte, 1024*16)

			for {
				readBytes, err := remoteConnection.Read(buff)
				if handleError(err, false) {
					break
				}
				_, err = localUDPConnection.WriteToUDP(buff[:readBytes], connectAddress)
				handleError(err, false)
				// if !handleError(err, false) {
				// logMessage(fmt.Sprintf("%s wrote %d bytes to %s", localUDPConnection.LocalAddr(), writenBytes, connectAddress))
				// }
			}
		}()

		// listen for incoming traffic from local app and forward it to remote machine
		wg.Add(1)
		go func() {
			defer wg.Done()

			buff := make([]byte, 1024*16)

			for {
				readBytes, _, err := localUDPConnection.ReadFromUDP(buff)
				if handleError(err, false) {
					break
				}
				_, err = remoteConnection.Write(buff[:readBytes])
				handleError(err, false)
				// if !handleError(err, false) {
				// 	logMessage(fmt.Sprintf("%s wrote %d bytes to %s", config.Connect, writenBytes, remoteConnection.RemoteAddr()))
				// }
			}
		}()

		// wait for go routines to finish
		wg.Wait()
	}
}

func main() {
	// load server config and certificates
	loadConfigFile(&config)
	loadCertificates(&config)
	config.TLSConfig.MinVersion = tls.VersionTLS13

	// create tcp listener on local machine
	localListener, err := tls.Listen("tcp", config.Listen, &config.TLSConfig)
	handleError(err, true)
	logMessage("listening on " + config.Listen)

	// accept new connections from remote machine
	for {
		remoteConnection, err := localListener.Accept()
		go handleRemoteClient(remoteConnection, err)
	}
}
