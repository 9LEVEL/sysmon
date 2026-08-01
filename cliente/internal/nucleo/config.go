package nucleo

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Host e um agente a consultar.
type Host struct {
	Nome  string `json:"nome"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

// Config e o config.json inteiro.
//
// Bruto guarda o documento original para que salvar de volta preserve chaves
// que esta versao nao conhece. Sem isso, abrir a tela de hosts numa versao
// antiga apagaria silenciosamente a configuracao escrita por uma mais nova.
type Config struct {
	Hosts     []Host
	Intervalo float64
	Timeout   float64
	Limiares  Limiares
	Bruto     map[string]any
}

// ErroConfig carrega mensagem destinada ao usuario final, nao ao log.
type ErroConfig struct{ Msg string }

func (e *ErroConfig) Error() string { return e.Msg }

const (
	IntervaloPadrao = 5.0
	TimeoutPadrao   = 4.0
)

// CaminhosPadrao devolve onde procurar o config, na ordem.
//
// O diretorio do executavel vem primeiro de proposito: e o que faz a pasta
// baixada do release funcionar sem instalar nada nem configurar caminho.
func CaminhosPadrao() []string {
	var out []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		out = append(out, filepath.Join(dir, "config.json"),
			filepath.Join(dir, "hosts.json"))
	}
	out = append(out, "config.json", "hosts.json")
	if lar, err := os.UserConfigDir(); err == nil {
		out = append(out, filepath.Join(lar, "sysmon", "config.json"))
	}
	if lar, err := os.UserHomeDir(); err == nil {
		out = append(out, filepath.Join(lar, ".config", "sysmon", "config.json"),
			filepath.Join(lar, ".sysmon.json"))
	}
	return out
}

// AcharConfig devolve o primeiro caminho existente, ou o indicado.
func AcharConfig(indicado string) string {
	if indicado != "" {
		return indicado
	}
	for _, c := range CaminhosPadrao() {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return "config.json"
}

// CarregarConfig le e valida o arquivo.
func CarregarConfig(caminho string) (Config, error) {
	dados, err := os.ReadFile(caminho)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, &ErroConfig{fmt.Sprintf(
				"config nao encontrado em: %s", caminho)}
		}
		return Config{}, &ErroConfig{fmt.Sprintf("nao consegui ler %s: %v",
			caminho, err)}
	}
	var bruto map[string]any
	if err := json.Unmarshal(dados, &bruto); err != nil {
		return Config{}, &ErroConfig{fmt.Sprintf(
			"%s nao e um JSON valido: %v", filepath.Base(caminho), err)}
	}
	return ConfigDe(bruto)
}

// ConfigDe valida um documento ja decodificado.
//
// Validar antes de gravar e o que permite a tela de hosts recusar entrada
// ruim sem ter destruido o arquivo que funcionava.
func ConfigDe(bruto map[string]any) (Config, error) {
	if bruto == nil {
		bruto = map[string]any{}
	}
	cfg := Config{
		Intervalo: IntervaloPadrao,
		Timeout:   TimeoutPadrao,
		Limiares:  LimiaresDe(bruto),
		Bruto:     bruto,
	}
	if v, ok := numero(bruto["intervalo"]); ok && v > 0 {
		cfg.Intervalo = v
	}
	if v, ok := numero(bruto["timeout"]); ok && v > 0 {
		cfg.Timeout = v
	}

	lista, _ := bruto["hosts"].([]any)
	vistos := map[string]bool{}
	for i, item := range lista {
		m, ok := item.(map[string]any)
		if !ok {
			return Config{}, &ErroConfig{fmt.Sprintf(
				"hosts[%d] nao e um objeto", i)}
		}
		h := Host{
			Nome:  texto(m["nome"]),
			URL:   strings.TrimSpace(texto(m["url"])),
			Token: texto(m["token"]),
		}
		if h.URL == "" {
			return Config{}, &ErroConfig{fmt.Sprintf(
				"hosts[%d] esta sem url", i)}
		}
		u, err := url.Parse(h.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return Config{}, &ErroConfig{fmt.Sprintf(
				"url invalida em hosts[%d]: %s", i, h.URL)}
		}
		// A URL do agente termina em /metrics; aceitar a raiz e deduzir
		// evita o erro mais comum de quem cola o endereco na mao.
		if u.Path == "" || u.Path == "/" {
			h.URL = strings.TrimRight(h.URL, "/") + "/metrics"
		}
		if h.Nome == "" {
			h.Nome = apelidoDaURL(h.URL)
		}
		// O nome e a chave em toda a interface: repetido, um host some.
		if vistos[h.Nome] {
			h.Nome = fmt.Sprintf("%s-%d", h.Nome, i+1)
		}
		vistos[h.Nome] = true
		cfg.Hosts = append(cfg.Hosts, h)
	}
	return cfg, nil
}

// SalvarConfig grava preservando o que esta versao nao conhece.
func SalvarConfig(caminho string, bruto map[string]any) error {
	dados, err := json.MarshalIndent(bruto, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(caminho)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return err
	}
	nome := tmp.Name()
	defer os.Remove(nome)
	if _, err := tmp.Write(append(dados, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// 0600 antes de publicar: o arquivo tem os tokens de todos os hosts, e
	// entre criar e proteger nao pode existir janela em que ele esteja legivel.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(nome, 0o600); err != nil {
			return err
		}
	}
	// Troca atomica: nunca existe um config pela metade no disco.
	return os.Rename(nome, caminho)
}

// AvisarPermissao devolve aviso quando o config com tokens esta legivel
// por outros usuarios. String vazia = sem problema.
func AvisarPermissao(caminho string) string {
	if runtime.GOOS == "windows" {
		return "" // o modelo de permissao la e outro; icacls resolve
	}
	st, err := os.Stat(caminho)
	if err != nil {
		return ""
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Sprintf("%s esta legivel por outros usuarios e contem "+
			"tokens. Corrija com: chmod 600 %s", caminho, caminho)
	}
	return ""
}

// ComoBruto reconstroi o documento a partir da config em memoria.
func (c Config) ComoBruto() map[string]any {
	out := map[string]any{}
	for k, v := range c.Bruto {
		out[k] = v
	}
	hosts := make([]any, 0, len(c.Hosts))
	for _, h := range c.Hosts {
		hosts = append(hosts, map[string]any{
			"nome": h.Nome, "url": h.URL, "token": h.Token,
		})
	}
	out["hosts"] = hosts
	alertas := map[string]any{}
	for nome, par := range c.Limiares.ComoMapa() {
		alertas[nome] = []any{par[0], par[1]}
	}
	out["alertas"] = alertas
	ign := make([]any, 0, len(c.Limiares.IgnorarMounts))
	for _, m := range c.Limiares.IgnorarMounts {
		ign = append(ign, m)
	}
	out["ignorar_mounts"] = ign
	return out
}

func texto(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// apelidoDaURL deriva um nome do endereco quando o usuario nao deu um.
func apelidoDaURL(bruta string) string {
	u, err := url.Parse(bruta)
	if err != nil {
		return "host"
	}
	nome := u.Hostname()
	if nome == "" {
		return "host"
	}
	// Um IP inteiro como nome polui a tela; o ultimo octeto basta para
	// distinguir hosts da mesma rede.
	if partes := strings.Split(nome, "."); len(partes) == 4 && ehIP(partes) {
		return "host-" + partes[3]
	}
	return strings.Split(nome, ".")[0]
}

func ehIP(partes []string) bool {
	for _, p := range partes {
		if p == "" || len(p) > 3 {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}
