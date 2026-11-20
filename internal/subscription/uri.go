package subscription

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"ipscan/internal/model"
)

// parseURIList parses a newline-separated list of share links. Broken lines
// become warnings instead of failing the whole subscription.
func parseURIList(s string) (*Document, error) {
	doc := &Document{Format: "URI 列表"}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		node, err := parseURI(line)
		if err != nil {
			if len(doc.Warnings) < 50 {
				doc.Warnings = append(doc.Warnings, fmt.Sprintf("%s… %v", clip(line, 40), err))
			}
			continue
		}
		if err := node.Validate(); err != nil {
			if len(doc.Warnings) < 50 {
				doc.Warnings = append(doc.Warnings, err.Error())
			}
			continue
		}
		doc.Nodes = append(doc.Nodes, node)
	}
	if len(doc.Nodes) == 0 {
		return nil, errors.New("没有可用的节点链接")
	}
	return doc, nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func parseURI(raw string) (model.Node, error) {
	scheme, body, ok := strings.Cut(strings.TrimSpace(raw), "://")
	if !ok {
		return model.Node{}, errors.New("不是节点链接")
	}
	switch strings.ToLower(scheme) {
	case "vmess":
		return parseVmess(body)
	case "vless":
		return parseStandardURL(raw, "vless")
	case "trojan":
		return parseStandardURL(raw, "trojan")
	case "ss":
		return parseSS(raw)
	case "ssr":
		return parseSSR(body)
	case "hysteria2", "hy2":
		return parseStandardURL(raw, "hysteria2")
	case "hysteria", "hy":
		return parseStandardURL(raw, "hysteria")
	case "tuic":
		return parseStandardURL(raw, "tuic")
	case "socks5", "socks":
		return parseStandardURL(raw, "socks5")
	case "http", "https":
		return parseStandardURL(raw, "http")
	default:
		return model.Node{}, fmt.Errorf("不支持的协议 %s", scheme)
	}
}

func nodeFromURL(raw, typ string) (model.Node, *url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return model.Node{}, nil, fmt.Errorf("链接格式错误: %w", err)
	}
	port := 0
	if p := u.Port(); p != "" {
		port, _ = strconv.Atoi(p)
	}
	n := model.Node{
		Type:   typ,
		Server: u.Hostname(),
		Port:   port,
		Raw:    raw,
	}
	n.Name = strings.TrimSpace(u.Fragment)
	if n.Name == "" {
		n.Name = n.Address()
	}
	if u.User != nil {
		n.UUID = u.User.Username()
		if pass, ok := u.User.Password(); ok {
			n.Password = pass
		}
	}
	return n, u, nil
}

func queryALPN(q url.Values) string {
	values := q["alpn"]
	if len(values) == 0 {
		return ""
	}
	return strings.Join(values, ",")
}

func parseStandardURL(raw, typ string) (model.Node, error) {
	n, u, err := nodeFromURL(raw, typ)
	if err != nil {
		return model.Node{}, err
	}
	q := u.Query()
	n.Network = firstNonEmpty(q.Get("type"), q.Get("network"), "tcp")
	n.Security = firstNonEmpty(q.Get("security"))

	switch typ {
	case "vless":
		n.Password = ""
		n.Flow = q.Get("flow")
		n.Security = firstNonEmpty(q.Get("security"), "none")
		n.ServerName = firstNonEmpty(q.Get("sni"), q.Get("peer"), q.Get("host"))
		n.PublicKey = q.Get("pbk")
		n.ShortID = q.Get("sid")
		n.Extra = map[string]string{}
		if enc := q.Get("encryption"); enc != "" && enc != "none" {
			n.Extra["encryption"] = enc
		}
		if spx := q.Get("spx"); spx != "" {
			n.Extra["spiderX"] = spx
		}
		if mode := q.Get("mode"); mode != "" {
			n.Extra["mode"] = mode
		}
		if htype := q.Get("headerType"); htype != "" && htype != "none" {
			n.Extra["headerType"] = htype
		}
	case "trojan":
		n.Password = n.UUID
		n.UUID = ""
		n.Security = firstNonEmpty(q.Get("security"), "tls")
		n.ServerName = firstNonEmpty(q.Get("sni"), q.Get("peer"), q.Get("host"))
	case "hysteria2":
		n.Password = firstNonEmpty(n.Password, n.UUID)
		n.UUID = ""
		n.Network = "quic"
		n.Security = "tls"
		n.ServerName = firstNonEmpty(q.Get("sni"), q.Get("peer"))
		n.Obfs = q.Get("obfs")
		n.ObfsPassword = q.Get("obfs-password")
		n.Extra = map[string]string{}
		if v := q.Get("pinSHA256"); v != "" {
			n.Extra["证书指纹"] = v
		}
		if v := q.Get("mport"); v != "" {
			n.Extra["端口跳变"] = v
		}
	case "hysteria":
		n.Network = "quic"
		n.Security = "tls"
		n.Password = firstNonEmpty(q.Get("auth"), q.Get("auth-str"), n.Password)
		n.ServerName = firstNonEmpty(q.Get("peer"), q.Get("sni"))
		n.Extra = map[string]string{}
		if v := firstNonEmpty(q.Get("protocol")); v != "" {
			n.Extra["protocol"] = v
		}
		if v := firstNonEmpty(q.Get("upmbps")); v != "" {
			n.Extra["上行"] = v + " Mbps"
		}
		if v := firstNonEmpty(q.Get("downmbps")); v != "" {
			n.Extra["下行"] = v + " Mbps"
		}
	case "tuic":
		n.Network = "quic"
		n.Security = "tls"
		n.ServerName = firstNonEmpty(q.Get("sni"), q.Get("peer"))
		if n.UUID == "" {
			n.UUID = q.Get("uuid")
		}
		if n.Password == "" {
			n.Password = q.Get("password")
		}
		n.Extra = map[string]string{}
		if v := q.Get("congestion_control"); v != "" {
			n.Extra["拥塞控制"] = v
		}
		if v := q.Get("udp_relay_mode"); v != "" {
			n.Extra["UDP 中继"] = v
		}
	case "socks5", "http":
		n.Network = "tcp"
		n.Security = "none"
	}

	n.Host = q.Get("host")
	n.Path = q.Get("path")
	n.ServiceName = firstNonEmpty(q.Get("serviceName"), q.Get("servicename"))
	n.ALPN = queryALPN(q)
	n.Fingerprint = q.Get("fp")
	n.Insecure = truthy(q.Get("allowInsecure")) || truthy(q.Get("insecure")) || truthy(q.Get("allow_insecure"))
	return n, nil
}

func parseVmess(body string) (model.Node, error) {
	decoded, ok := decodeBase64Bytes(strings.TrimSpace(body))
	if !ok {
		return model.Node{}, errors.New("vmess 内容不是合法的 base64")
	}
	dec := json.NewDecoder(strings.NewReader(string(decoded)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return model.Node{}, fmt.Errorf("vmess JSON 解析失败: %w", err)
	}
	n := model.Node{
		Type:        "vmess",
		Server:      str(m, "add"),
		Port:        num(m, "port"),
		UUID:        str(m, "id"),
		AlterID:     num(m, "aid"),
		Cipher:      str(m, "scy"),
		Network:     firstNonEmpty(str(m, "net"), "tcp"),
		Security:    firstNonEmpty(str(m, "tls"), "none"),
		Host:        str(m, "host"),
		Path:        str(m, "path"),
		ServerName:  firstNonEmpty(str(m, "sni"), str(m, "host")),
		ALPN:        str(m, "alpn"),
		Fingerprint: str(m, "fp"),
		Name:        str(m, "ps"),
	}
	n.Insecure = truthy(str(m, "allowInsecure"))
	if htype := str(m, "type"); htype != "" && htype != "none" {
		if n.Extra == nil {
			n.Extra = map[string]string{}
		}
		n.Extra["伪装类型"] = htype
	}
	if n.Name == "" {
		n.Name = n.Address()
	}
	return n, nil
}

// parseSS handles all three Shadowsocks link layouts: SIP002 with the userinfo
// as base64, legacy with the whole payload as base64, and plain method:pass.
func parseSS(raw string) (model.Node, error) {
	body := strings.TrimPrefix(raw, "ss://")
	n := model.Node{Type: "ss", Raw: raw}

	var fragment string
	if i := strings.IndexByte(body, '#'); i >= 0 {
		fragment, body = body[i+1:], body[:i]
	}
	if name, err := url.QueryUnescape(fragment); err == nil && name != "" {
		n.Name = name
	}

	var query url.Values
	if i := strings.IndexByte(body, '?'); i >= 0 {
		if q, err := url.ParseQuery(body[i+1:]); err == nil {
			query = q
		}
		body = body[:i]
	}

	var userinfo, hostport string
	if at := strings.LastIndexByte(body, '@'); at >= 0 {
		userinfo, hostport = body[:at], body[at+1:]
		if !strings.Contains(userinfo, ":") {
			if decoded, ok := decodeBase64Bytes(userinfo); ok {
				userinfo = string(decoded)
			}
		}
	} else {
		decoded, ok := decodeBase64Bytes(body)
		if !ok {
			return model.Node{}, errors.New("ss 内容不是合法的 base64")
		}
		plain := string(decoded)
		at := strings.LastIndexByte(plain, '@')
		if at < 0 {
			return model.Node{}, errors.New("ss 链接缺少服务器地址")
		}
		userinfo, hostport = plain[:at], plain[at+1:]
	}

	if unescaped, err := url.PathUnescape(userinfo); err == nil {
		userinfo = unescaped
	}
	method, password, ok := strings.Cut(userinfo, ":")
	if !ok {
		return model.Node{}, errors.New("ss 链接缺少加密方式或密码")
	}
	n.Cipher, n.Password = method, password

	hostport = strings.TrimSuffix(strings.TrimSpace(hostport), "/")
	if host, portStr, err := splitHostPort(hostport); err == nil {
		n.Server = host
		n.Port = portStr
	} else {
		return model.Node{}, err
	}

	if query != nil {
		if plugin := query.Get("plugin"); plugin != "" {
			parts := strings.Split(plugin, ";")
			n.Plugin = parts[0]
			n.PluginOpts = strings.Join(parts[1:], ";")
		}
	}
	if n.Name == "" {
		n.Name = n.Address()
	}
	return n, nil
}

func splitHostPort(hostport string) (string, int, error) {
	hostport = strings.TrimSpace(hostport)
	u, err := url.Parse("//" + hostport)
	if err != nil || u.Hostname() == "" {
		return "", 0, fmt.Errorf("服务器地址无效: %s", hostport)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		return "", 0, fmt.Errorf("端口无效: %s", hostport)
	}
	return u.Hostname(), port, nil
}

func parseSSR(body string) (model.Node, error) {
	decoded, ok := decodeBase64Bytes(strings.TrimSpace(body))
	if !ok {
		return model.Node{}, errors.New("ssr 内容不是合法的 base64")
	}
	plain := string(decoded)
	main, rawQuery, _ := strings.Cut(plain, "/?")
	fields := strings.Split(main, ":")
	if len(fields) < 6 {
		return model.Node{}, errors.New("ssr 链接字段不完整")
	}
	port, err := strconv.Atoi(fields[1])
	if err != nil {
		return model.Node{}, errors.New("ssr 端口无效")
	}
	pass, _ := decodeBase64Bytes(fields[5])
	n := model.Node{
		Type:     "ssr",
		Server:   fields[0],
		Port:     port,
		Password: string(pass),
		Cipher:   fields[3],
		Obfs:     fields[4],
		Network:  "tcp",
		Security: "none",
		Raw:      "ssr://" + body,
		Extra:    map[string]string{"协议": fields[2]},
	}
	if rawQuery != "" {
		q, _ := url.ParseQuery(rawQuery)
		if remarks := q.Get("remarks"); remarks != "" {
			if b, ok := decodeBase64Bytes(remarks); ok {
				n.Name = string(b)
			}
		}
		if v := q.Get("obfsparam"); v != "" {
			if b, ok := decodeBase64Bytes(v); ok {
				n.Extra["混淆参数"] = string(b)
			}
		}
		if v := q.Get("protoparam"); v != "" {
			if b, ok := decodeBase64Bytes(v); ok {
				n.Extra["协议参数"] = string(b)
			}
		}
	}
	if n.Name == "" {
		n.Name = n.Address()
	}
	return n, nil
}
