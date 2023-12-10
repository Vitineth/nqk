package internal

import "fmt"

const (
	LabelGlobalDomain = "org.xiomi.nqkd.domain"
	LabelPortDomain   = "org.xiomi.nqkd.$port.domain"

	LabelGlobalNonstandardHttp = "org.xiomi.nqkd.http.nonstandard"
	LabelPortNonstandardHttp   = "org.xiomi.nqkd.$port.http.nonstandard"

	LabelGlobalSsl = "org.xiomi.nqkd.ssl"
	LabelPortSsl   = "org.xiomi.nqkd.$port.ssl"

	LabelGlobalBind = "org.xiomi.nqkd.bind"
	LabelPortBind   = "org.xiomi.nqkd.$port.bind"

	LabelGlobalType = "org.xiomi.nqkd.type"
	LabelPortType   = "org.xiomi.nqkd.$port.type"

	LabelGlobalPortOverride = "org.xiomi.nqkd.port.override"
	LabelPortPortOverride   = "org.xiomi.nqkd.$port.port.override"

	LabelPortHide = "org.xiomi.nqkd.$port.hide"

	ValueTypeHttp  = "http"
	ValueTypeHttps = "https"
	ValueTypeTcp   = "tcp"
	ValueTypeUdp   = "udp"

	NginxConfigHttp = `server {
    listen %s;
    %s
    server_name %s;
    location / {
        proxy_pass %s://%s:%d;

		# WebSocket support
		proxy_http_version 1.1;
		proxy_set_header Upgrade $http_upgrade;
		proxy_set_header Connection $http_connection;
    }
}
`
	NginxConfigTcp = `server {
    listen %s;
    %s
    proxy_pass %s:%d;
}
`
	NginxConfigUdp = `server {
    listen %s udp;
    proxy_pass %s:%d;
}
`
	NginxConfigSsl = `ssl_certificate %s;
ssl_certificate_key %s;
ssl_protocols TLSv1.3;
ssl_ciphers     HIGH:!aNULL:!MD5;`
)

func NginxSsl(config BindingConfiguration) string {
	return fmt.Sprintf(NginxConfigSsl, config.SslCertificate, config.SslPrivateKey)
}

func NginxHttp(listen string, useSsl bool, domain string, protocol string, ip string, targetPort uint16, config BindingConfiguration) string {
	ssl := ""
	if useSsl {
		ssl = NginxSsl(config)
	}

	realListen := listen
	if useSsl {
		realListen += " ssl"
	}

	return fmt.Sprintf(
		NginxConfigHttp,
		realListen,
		ssl,
		domain,
		protocol,
		ip,
		targetPort,
	)
}

func NginxTcp(listen string, useSsl bool, ip string, port uint16, config BindingConfiguration) string {
	ssl := ""
	if useSsl {
		ssl = NginxSsl(config)
	}

	return fmt.Sprintf(
		NginxConfigTcp,
		listen,
		ssl,
		ip,
		port,
	)
}

func NginxUdp(listen string, ip string, port uint16) string {
	return fmt.Sprintf(
		NginxConfigUdp,
		listen,
		ip,
		port,
	)
}

type BindingConfiguration struct {
	DefaultDomain  string
	SslCertificate string
	SslPrivateKey  string
}
