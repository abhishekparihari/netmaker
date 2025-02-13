package functions

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/gravitl/netmaker/logger"
	"github.com/gravitl/netmaker/netclient/config"
	"github.com/gravitl/netmaker/netclient/ncutils"
	"github.com/gravitl/netmaker/tls"
)

// Register - the function responsible for registering with the server and acquiring certs
func Register(cfg *config.ClientConfig, key string) error {

	//generate new key if one doesn' exist
	var private *ed25519.PrivateKey
	var err error
	private, err = tls.ReadKey(ncutils.GetNetclientPath() + ncutils.GetSeparator() + "client.key")
	if err != nil {
		_, newKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return err
		}
		if err := tls.SaveKey(ncutils.GetNetclientPath(), ncutils.GetSeparator()+"client.key", newKey); err != nil {
			return err
		}
		private = &newKey
	}
	//check if cert exists
	_, err = tls.ReadCert(ncutils.GetNetclientServerPath(cfg.Server.Server) + ncutils.GetSeparator() + "client.pem")
	if errors.Is(err, os.ErrNotExist) {
		if err := RegisterWithServer(private, cfg); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return nil
}

// RegisterWithServer calls the register endpoint with privatekey and commonname - api returns ca and client certificate
func RegisterWithServer(private *ed25519.PrivateKey, cfg *config.ClientConfig) error {
	data := config.RegisterRequest{
		Key:        *private,
		CommonName: tls.NewCName(cfg.Node.Name),
	}
	url := "https://" + cfg.Server.API + "/api/server/register"
	logger.Log(1, "register at "+url)

	token, err := Authenticate(cfg)
	if err != nil {
		return err
	}
	response, err := API(data, http.MethodPost, url, token)
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return errors.New(response.Status)
	}
	var resp config.RegisterResponse
	if err := json.NewDecoder(response.Body).Decode(&resp); err != nil {
		return errors.New("unmarshal cert error " + err.Error())
	}

	// set broker information on register
	var modServer bool
	if resp.Broker != "" && resp.Broker != cfg.Server.Server {
		cfg.Server.Server = resp.Broker
		modServer = true
	}
	if resp.Port != "" && resp.Port != cfg.Server.MQPort {
		cfg.Server.MQPort = resp.Port
		modServer = true
	}
	if modServer {
		if err = config.ModServerConfig(&cfg.Server, cfg.Node.Network); err != nil {
			logger.Log(0, "error overwriting config with broker information: "+err.Error())
		}
	}

	//x509.Certificate.PublicKey is an interface so json encoding/decoding results in a string rather that []byte
	//the pubkeys are included in the response so the values in the certificate can be updated appropriately
	resp.CA.PublicKey = resp.CAPubKey
	resp.Cert.PublicKey = resp.CertPubKey
	if err := tls.SaveCert(ncutils.GetNetclientServerPath(cfg.Server.Server)+ncutils.GetSeparator(), "root.pem", &resp.CA); err != nil {
		return err
	}
	if err := tls.SaveCert(ncutils.GetNetclientServerPath(cfg.Server.Server)+ncutils.GetSeparator(), "client.pem", &resp.Cert); err != nil {
		return err
	}
	logger.Log(0, "certificates/key saved ")
	//join the network defined in the token
	return nil
}
