package subscription

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ipscan/internal/model"
)

// parseConfigJSON reads an Xray/v2ray client config or a sing-box config and
// keeps the proxy outbounds. Routing, inbounds and rules are ignored.
func parseConfigJSON(data []byte) (*Document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("JSON 解析失败: %w", err)
	}
	outbounds := list(root, "outbounds")
	if len(outbounds) == 0 {
		return nil, errors.New("JSON 里没有 outbounds")
	}

	doc := &Document{Format: "Xray JSON"}
	for _, item := range outbounds {
		o, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var node model.Node
		var known bool
		format := "Xray JSON"
		if _, isXray := o["streamSettings"]; isXray {
			node, known = fromXrayOutbound(o)
		} else if _, isXray := o["settings"]; isXray {
			node, known = fromXrayOutbound(o)
		} else {
			node, known = fromSingBoxOutbound(o)
			format = "sing-box JSON"
		}
		if !known {
			continue
		}
		if len(doc.Nodes) == 0 {
			doc.Format = format
		}
		if err := node.Validate(); err != nil {
			doc.Warnings = append(doc.Warnings, err.Error())
			continue
		}
		doc.Nodes = append(doc.Nodes, node)
	}
	if len(doc.Nodes) == 0 {
		return nil, errors.New("outbounds 里没有可识别的代理")
	}
	return doc, nil
}

func firstMap(items []any) map[string]any {
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			return m
		}
	}
	return nil
}

func fromXrayOutbound(o map[string]any) (model.Node, bool) {
	proto := strings.ToLower(str(o, "protocol"))
	switch proto {
	case "vmess", "vless", "trojan", "shadowsocks", "socks", "http":
	default:
		return model.Node{}, false
	}
	if proto == "shadowsocks" {
		proto = "ss"
	}
	if proto == "socks" {
		proto = "socks5"
	}

	settings := obj(o, "settings")
	n := model.Node{
		Type:  proto,
		Name:  str(o, "tag"),
		Raw:   "",
		Extra: map[string]string{},
	}

	var server string
	var port int
	switch proto {
	case "vmess":
		vnext := firstMap(list(settings, "vnext"))
		user := firstMap(list(vnext, "users"))
		server, port = str(vnext, "address"), num(vnext, "port")
		n.UUID = str(user, "id")
		n.AlterID = num(user, "alterId")
		n.Cipher = str(user, "security")
		n.Flow = str(user, "flow")
	case "vless":
		vnext := firstMap(list(settings, "vnext"))
		user := firstMap(list(vnext, "users"))
		server, port = str(vnext, "address"), num(vnext, "port")
		n.UUID = str(user, "id")
		n.Flow = str(user, "flow")
		n.Cipher = str(user, "encryption")
		if n.Cipher == "none" {
			n.Cipher = ""
		}
	case "trojan":
		srv := firstMap(list(settings, "servers"))
		server, port = str(srv, "address"), num(srv, "port")
		n.Password = str(srv, "password")
	case "ss":
		srv := firstMap(list(settings, "servers"))
		server, port = str(srv, "address"), num(srv, "port")
		n.Password = str(srv, "password")
		n.Cipher = str(srv, "method")
	case "socks5", "http":
		srv := firstMap(list(settings, "servers"))
		server, port = str(srv, "address"), num(srv, "port")
		user := firstMap(list(srv, "users"))
		n.UUID = firstNonEmpty(str(user, "user"), str(srv, "user"))
		n.Password = firstNonEmpty(str(user, "pass"), str(srv, "pass"))
	}
	n.Server, n.Port = server, port

	stream := obj(o, "streamSettings")
	network := firstNonEmpty(str(stream, "network"), "tcp")
	n.Network = network
	security := firstNonEmpty(str(stream, "security"), "none")
	n.Security = security

	tls := obj(stream, "tlsSettings", "xtlsSettings")
	reality := obj(stream, "realitySettings")
	if security == "reality" || str(reality, "publicKey") != "" {
		n.Security = "reality"
		n.PublicKey = str(reality, "publicKey")
		n.ShortID = str(reality, "shortId")
		n.Fingerprint = str(reality, "fingerprint")
		n.ServerName = str(reality, "serverName")
		n.Insecure = boolean(reality, "allowInsecure")
	}
	if n.ServerName == "" {
		n.ServerName = str(tls, "serverName")
	}
	if n.Fingerprint == "" {
		n.Fingerprint = str(tls, "fingerprint")
	}
	if n.ALPN == "" {
		n.ALPN = str(tls, "alpn")
	}
	if !n.Insecure {
		n.Insecure = boolean(tls, "allowInsecure")
	}

	switch network {
	case "ws":
		ws := obj(stream, "wsSettings")
		n.Path = str(ws, "path")
		if headers := obj(ws, "headers"); headers != nil {
			n.Host = firstNonEmpty(str(headers, "Host"), str(headers, "host"))
		}
	case "grpc":
		n.ServiceName = str(obj(stream, "grpcSettings"), "serviceName")
	case "http", "h2", "httpupgrade", "xhttp", "splithttp":
		switch network {
		case "httpupgrade":
			hu := obj(stream, "httpupgradeSettings")
			n.Path = str(hu, "path")
			n.Host = str(hu, "host")
		case "xhttp", "splithttp":
			xs := obj(stream, "xhttpSettings", "splithttpSettings")
			n.Path = str(xs, "path")
			n.Host = str(xs, "host")
		default:
			hs := obj(stream, "httpSettings")
			n.Path = str(hs, "path")
			n.Host = str(hs, "host")
		}
	}
	if n.ServerName == "" {
		n.ServerName = n.Host
	}
	if n.Name == "" {
		n.Name = n.Address()
	}
	cleanExtra(&n)
	return n, true
}

func fromSingBoxOutbound(o map[string]any) (model.Node, bool) {
	typ := strings.ToLower(str(o, "type"))
	switch typ {
	case "vmess", "vless", "trojan", "shadowsocks", "socks", "http", "hysteria2", "tuic":
	case "wireguard", "direct", "block", "dns", "selector", "urltest", "hysteria":
		return model.Node{}, false
	default:
		return model.Node{}, false
	}
	if typ == "shadowsocks" {
		typ = "ss"
	}
	if typ == "socks" {
		typ = "socks5"
	}

	n := model.Node{
		Type:        typ,
		Name:        str(o, "tag"),
		Server:      str(o, "server"),
		Port:        num(o, "server_port"),
		UUID:        str(o, "uuid"),
		Password:    str(o, "password"),
		Cipher:      str(o, "method"),
		AlterID:     num(o, "alter_id"),
		Flow:        str(o, "flow"),
		Network:     "tcp",
		Security:    "none",
		Obfs:        str(obj(o, "obfs"), "type"),
		ObfsPassword: str(obj(o, "obfs"), "password"),
		Extra:       map[string]string{},
	}

	if tls := obj(o, "tls"); tls != nil {
		if boolean(tls, "enabled") {
			n.Security = "tls"
		}
		n.ServerName = str(tls, "server_name")
		n.ALPN = str(tls, "alpn")
		n.Insecure = boolean(tls, "insecure")
		n.Fingerprint = str(obj(tls, "utls"), "fingerprint")
		if reality := obj(tls, "reality"); reality != nil && boolean(reality, "enabled") {
			n.Security = "reality"
			n.PublicKey = str(reality, "public_key")
			n.ShortID = str(reality, "short_id")
		}
	}

	if tr := obj(o, "transport"); tr != nil {
		if t := str(tr, "type"); t != "" {
			n.Network = t
		}
		n.Path = str(tr, "path")
		n.ServiceName = str(tr, "service_name")
		if headers := obj(tr, "headers"); headers != nil {
			n.Host = firstNonEmpty(str(headers, "Host"), str(headers, "host"))
		}
		if n.Host == "" {
			n.Host = str(tr, "host")
		}
		if n.Path == "" {
			n.Path = str(tr, "path")
		}
	}
	if typ == "hysteria2" || typ == "tuic" {
		n.Network = "quic"
	}

	switch typ {
	case "hysteria2":
		if v := str(obj(o, "obfs"), "type"); v != "" {
			n.Obfs = v
		}
	case "tuic":
		n.Extra["拥塞控制"] = str(o, "congestion_control")
		n.Extra["UDP 中继"] = str(o, "udp_relay_mode")
	}
	if up := num(o, "up_mbps"); up > 0 {
		n.Extra["上行"] = strconv.Itoa(up)
	}
	if down := num(o, "down_mbps"); down > 0 {
		n.Extra["下行"] = strconv.Itoa(down)
	}
	if n.ServerName == "" {
		n.ServerName = n.Host
	}
	if n.Name == "" {
		n.Name = n.Address()
	}
	cleanExtra(&n)
	return n, true
}

func cleanExtra(n *model.Node) {
	if len(n.Extra) == 0 {
		return
	}
	for k, v := range n.Extra {
		if strings.TrimSpace(v) == "" {
			delete(n.Extra, k)
		}
	}
	if len(n.Extra) == 0 {
		n.Extra = nil
	}
}
