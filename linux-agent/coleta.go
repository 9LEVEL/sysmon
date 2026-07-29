// Leitores de /sys e /proc. Nada aqui executa comando externo nem escreve:
// toda funcao abre um arquivo, interpreta e devolve. Em qualquer erro devolve
// o zero-value em vez de propagar - um sensor ausente nao pode derrubar a
// coleta inteira, e host nenhum expoe o mesmo conjunto de arquivos.
package main

import (
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	setor          = 512 // /proc/diskstats conta setores de 512B, sempre
	maxExtraBytes  = 256 << 10
	maxArquivoProc = 1 << 20
)

// fsReais lista filesystems que representam armazenamento de verdade. Tudo que
// nao esta aqui (tmpfs, overlay, cgroup, autofs, squashfs de snap...) e ruido.
var fsReais = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "jfs": true, "reiserfs": true, "vfat": true,
	"exfat": true, "ntfs": true, "ntfs3": true, "fuseblk": true, "ufs": true,
	"nfs": true, "nfs4": true, "cifs": true,
}

// NetIgnorarPadrao: prefixos das interfaces virtuais que Proxmox e Docker
// criam aos montes. Sem isso um host com 20 VMs devolve 60 interfaces.
var NetIgnorarPadrao = []string{
	"lo", "veth", "tap", "fwbr", "fwln", "fwpr", "docker", "br-",
	"virbr", "vnet", "cali", "flannel",
}

// Fontes localiza os arquivos do sistema. Em producao raiz e "";
// nos testes e um diretorio temporario com um /sys e /proc falsos.
type Fontes struct {
	raiz        string
	NetIgnorar  []string
	MountsFixos []string // nao-vazio desliga a descoberta automatica
}

// P resolve um caminho absoluto do sistema dentro da raiz configurada.
func (f Fontes) P(partes ...string) string {
	if f.raiz == "" {
		return filepath.Join(partes...)
	}
	return filepath.Join(append([]string{f.raiz}, partes...)...)
}

// ---------------------------------------------------------------- primitivas

// ler devolve o conteudo aparado. O caminho ja deve estar resolvido.
func ler(caminho string, limite int64) (string, bool) {
	fh, err := os.Open(caminho)
	if err != nil {
		return "", false
	}
	defer fh.Close()
	b, err := io.ReadAll(io.LimitReader(fh, limite))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func lerInt(caminho string) (int64, bool) {
	s, ok := ler(caminho, 64)
	if !ok {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}

func arred(v float64, casas int) float64 {
	p := math.Pow10(casas)
	return math.Round(v*p) / p
}

func f64(v float64) *float64 { return &v }
func i64(v int64) *int64     { return &v }

// milli converte milicelsius do sysfs para graus com uma casa.
func milli(v int64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return f64(arred(float64(v)/1000, 1))
}

// taxa devolve o delta por segundo, tolerando contador que zerou (reboot da
// interface, wrap de 32 bits). Nesses casos devolve nil em vez de um pico falso.
func taxa(atual, anterior int64, temAnterior bool, dt float64) *float64 {
	if !temAnterior || dt <= 0 || atual < anterior {
		return nil
	}
	return f64(arred(float64(atual-anterior)/dt, 1))
}

// campos parte a linha em espacos, ignorando repeticoes.
func campos(linha string) []string { return strings.Fields(linha) }

// ---------------------------------------------------------------- temperatura

func (f Fontes) Temps() []Sensor {
	out := []Sensor{}
	chips, _ := filepath.Glob(f.P("/sys/class/hwmon", "hwmon*"))
	for _, hw := range chips {
		chip, ok := ler(filepath.Join(hw, "name"), 256)
		if !ok || chip == "" {
			chip = filepath.Base(hw)
		}
		entradas, _ := filepath.Glob(filepath.Join(hw, "temp*_input"))
		for _, inp := range entradas {
			bruto, ok := lerInt(inp)
			if !ok {
				continue
			}
			pre := strings.TrimSuffix(filepath.Base(inp), "_input")
			rotulo, ok := ler(filepath.Join(hw, pre+"_label"), 256)
			if !ok || rotulo == "" {
				rotulo = pre
			}
			out = append(out, Sensor{
				Chip:  chip,
				Label: rotulo,
				C:     arred(float64(bruto)/1000, 1),
				Crit:  milli(lerInt(filepath.Join(hw, pre+"_crit"))),
				Max:   milli(lerInt(filepath.Join(hw, pre+"_max"))),
			})
		}
	}

	// Fallback para maquinas sem hwmon exposto (VMs, algumas placas OEM).
	if len(out) == 0 {
		zonas, _ := filepath.Glob(f.P("/sys/class/thermal", "thermal_zone*"))
		for _, z := range zonas {
			bruto, ok := lerInt(filepath.Join(z, "temp"))
			if !ok {
				continue
			}
			tipo, ok := ler(filepath.Join(z, "type"), 256)
			if !ok || tipo == "" {
				tipo = filepath.Base(z)
			}
			out = append(out, Sensor{Chip: "thermal_zone", Label: tipo,
				C: arred(float64(bruto)/1000, 1)})
		}
	}
	return out
}

// SensorCPU escolhe o sensor mais representativo da CPU. A ordem de preferencia
// cobre Intel (Package id 0), AMD (Tctl/Tdie) e ARM (cpu_thermal); o ultimo
// recurso e o sensor mais quente da maquina, que quase sempre e a CPU.
func SensorCPU(lista []Sensor) *Sensor {
	prefer := []string{"package id 0", "tctl", "tdie", "cpu"}
	for _, alvo := range prefer {
		for i := range lista {
			if strings.HasPrefix(strings.ToLower(lista[i].Label), alvo) {
				return &lista[i]
			}
		}
	}
	chipsCPU := map[string]bool{
		"coretemp": true, "k10temp": true, "zenpower": true, "cpu_thermal": true,
	}
	var melhor *Sensor
	for i := range lista {
		if !chipsCPU[lista[i].Chip] {
			continue
		}
		if melhor == nil || lista[i].C > melhor.C {
			melhor = &lista[i]
		}
	}
	if melhor != nil {
		return melhor
	}
	for i := range lista {
		if melhor == nil || lista[i].C > melhor.C {
			melhor = &lista[i]
		}
	}
	return melhor
}

func (f Fontes) Fans() map[string]int64 {
	out := map[string]int64{}
	chips, _ := filepath.Glob(f.P("/sys/class/hwmon", "hwmon*"))
	for _, hw := range chips {
		chip, ok := ler(filepath.Join(hw, "name"), 256)
		if !ok || chip == "" {
			chip = filepath.Base(hw)
		}
		entradas, _ := filepath.Glob(filepath.Join(hw, "fan*_input"))
		for _, inp := range entradas {
			rpm, ok := lerInt(inp)
			if !ok || rpm == 0 { // 0 RPM = ventoinha parada ou canal nao usado
				continue
			}
			pre := strings.TrimSuffix(filepath.Base(inp), "_input")
			rotulo, ok := ler(filepath.Join(hw, pre+"_label"), 256)
			if !ok || rotulo == "" {
				rotulo = pre
			}
			out[chip+"/"+rotulo] = rpm
		}
	}
	return out
}

// ---------------------------------------------------------------- cpu / mem

// CPUBruto devolve (idle, total) acumulados da primeira linha de /proc/stat.
// Sao contadores; o uso percentual sai do delta entre duas leituras.
func (f Fontes) CPUBruto() (idle, total int64, ok bool) {
	texto, ok := ler(f.P("/proc/stat"), maxArquivoProc)
	if !ok {
		return 0, 0, false
	}
	linha, _, _ := strings.Cut(texto, "\n")
	c := campos(linha)
	if len(c) < 5 || c[0] != "cpu" {
		return 0, 0, false
	}
	for i, s := range c[1:] {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += v
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	return idle, total, true
}

func (f Fontes) CPUInfo() (modelo string, mhz *int64) {
	texto, _ := ler(f.P("/proc/cpuinfo"), 256<<10)
	for _, linha := range strings.Split(texto, "\n") {
		if strings.HasPrefix(linha, "model name") ||
			strings.HasPrefix(linha, "Model") ||
			strings.HasPrefix(linha, "Hardware") {
			if _, v, ok := strings.Cut(linha, ":"); ok {
				modelo = strings.TrimSpace(v)
				break
			}
		}
	}
	if khz, ok := lerInt(f.P("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq")); ok {
		mhz = i64(int64(math.Round(float64(khz) / 1000)))
	}
	return modelo, mhz
}

func (f Fontes) Load() [3]float64 {
	texto, ok := ler(f.P("/proc/loadavg"), 256)
	if !ok {
		return [3]float64{}
	}
	c := campos(texto)
	var out [3]float64
	for i := 0; i < 3 && i < len(c); i++ {
		v, err := strconv.ParseFloat(c[i], 64)
		if err == nil {
			out[i] = v
		}
	}
	return out
}

func (f Fontes) UptimeS() int64 {
	texto, ok := ler(f.P("/proc/uptime"), 256)
	if !ok {
		return 0
	}
	c := campos(texto)
	if len(c) == 0 {
		return 0
	}
	v, err := strconv.ParseFloat(c[0], 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func (f Fontes) Mem() Mem {
	texto, _ := ler(f.P("/proc/meminfo"), 64<<10)
	info := map[string]int64{}
	for _, linha := range strings.Split(texto, "\n") {
		chave, resto, ok := strings.Cut(linha, ":")
		if !ok {
			continue
		}
		c := campos(resto)
		if len(c) == 0 {
			continue
		}
		v, err := strconv.ParseInt(c[0], 10, 64)
		if err != nil {
			continue
		}
		info[chave] = v * 1024 // /proc/meminfo reporta em kB
	}

	total := info["MemTotal"]
	// MemAvailable existe desde o kernel 3.14. Sem o fallback, um kernel antigo
	// (ou um /proc podado dentro de container) faria o agente reportar 100% de
	// RAM usada para sempre.
	disp, tem := info["MemAvailable"]
	if !tem {
		disp = info["MemFree"] + info["Cached"] + info["Buffers"]
	}
	if total > 0 && disp > total {
		disp = total
	}

	m := Mem{
		Total:     total,
		Usado:     total - disp,
		Cache:     info["Cached"] + info["Buffers"],
		SwapTotal: info["SwapTotal"],
		SwapUsado: info["SwapTotal"] - info["SwapFree"],
	}
	if total > 0 {
		m.Percent = f64(arred(100*float64(m.Usado)/float64(total), 1))
	}
	if m.SwapTotal > 0 {
		m.SwapPercent = f64(arred(100*float64(m.SwapUsado)/float64(m.SwapTotal), 1))
	}
	return m
}

// Pressure le o PSI do kernel: quanto tempo as tarefas ficaram *travadas*
// esperando CPU, IO ou memoria. E o melhor sinal isolado de "esse host esta
// sofrendo" - melhor que load average, que nao distingue trabalho de espera.
// Requer kernel >= 4.20 com CONFIG_PSI; devolve nil quando ausente.
func (f Fontes) Pressure() map[string]map[string]float64 {
	out := map[string]map[string]float64{}
	for _, recurso := range []string{"cpu", "io", "memory"} {
		texto, ok := ler(f.P("/proc/pressure", recurso), 4096)
		if !ok {
			continue
		}
		medidas := map[string]float64{}
		for _, linha := range strings.Split(texto, "\n") {
			c := campos(linha)
			if len(c) < 2 {
				continue
			}
			tipo := c[0] // "some" ou "full"
			for _, par := range c[1:] {
				chave, valor, ok := strings.Cut(par, "=")
				if !ok || (chave != "avg10" && chave != "avg60") {
					continue
				}
				if v, err := strconv.ParseFloat(valor, 64); err == nil {
					medidas[tipo+"_"+chave] = v
				}
			}
		}
		if len(medidas) > 0 {
			out[recurso] = medidas
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ---------------------------------------------------------------- disco

// desescapar reverte o escape octal que o kernel aplica em /proc/mounts.
func desescapar(s string) string {
	return strings.NewReplacer(
		`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`,
	).Replace(s)
}

// DescobrirMounts devolve os pontos de montagem com filesystem de verdade.
// Substitui a lista fixa "/ /var/lib/vz" da v1, que so fazia sentido no Proxmox.
func (f Fontes) DescobrirMounts() []string {
	if len(f.MountsFixos) > 0 {
		return f.MountsFixos
	}
	texto, _ := ler(f.P("/proc/mounts"), maxArquivoProc)
	out := []string{}
	for _, linha := range strings.Split(texto, "\n") {
		c := campos(linha)
		if len(c) < 3 || !fsReais[c[2]] {
			continue
		}
		out = append(out, desescapar(c[1]))
	}
	return out
}

// Discos consulta statfs por ponto de montagem, deduplicando filesystems iguais
// por st_dev (bind mounts e subvolumes apontam para o mesmo device).
func (f Fontes) Discos(pontos []string) []Disco {
	out := []Disco{}
	vistos := map[uint64]bool{}
	for _, mp := range pontos {
		caminho := f.P(mp)
		var st syscall.Stat_t
		if err := syscall.Stat(caminho, &st); err != nil {
			continue
		}
		if vistos[uint64(st.Dev)] {
			continue
		}
		var fs syscall.Statfs_t
		if err := syscall.Statfs(caminho, &fs); err != nil {
			continue
		}
		total := int64(fs.Blocks) * fs.Bsize
		livre := int64(fs.Bavail) * fs.Bsize
		if total <= 0 {
			continue
		}
		vistos[uint64(st.Dev)] = true

		d := Disco{
			Mount:   mp,
			Total:   total,
			Usado:   total - livre,
			Percent: arred(100*float64(total-livre)/float64(total), 1),
		}
		// Disco cheio de inodes falha igual a disco cheio de bytes, e o df -h
		// nao mostra. Vale reportar.
		if fs.Files > 0 {
			usados := int64(fs.Files) - int64(fs.Ffree)
			d.InodesPercent = f64(arred(100*float64(usados)/float64(fs.Files), 1))
		}
		out = append(out, d)
	}
	return out
}

// DiskIOBruto le os contadores de /proc/diskstats, so para discos inteiros.
// Particoes, loop devices e mapeamentos do LVM sao filtrados: contariam duas
// vezes o mesmo IO fisico.
func (f Fontes) DiskIOBruto() map[string]AmostraIO {
	out := map[string]AmostraIO{}
	texto, _ := ler(f.P("/proc/diskstats"), maxArquivoProc)
	for _, linha := range strings.Split(texto, "\n") {
		c := campos(linha)
		if len(c) < 14 {
			continue
		}
		nome := c[2]
		if temPrefixo(nome, "loop", "ram", "zram", "dm-", "sr", "md") {
			continue
		}
		// /sys/block so lista discos inteiros; particao nao aparece.
		if _, err := os.Stat(f.P("/sys/block", nome)); err != nil {
			continue
		}
		lidos, e1 := strconv.ParseInt(c[5], 10, 64)
		escritos, e2 := strconv.ParseInt(c[9], 10, 64)
		ioms, e3 := strconv.ParseInt(c[12], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		out[nome] = AmostraIO{
			LidosB:    lidos * setor,
			EscritosB: escritos * setor,
			IOms:      ioms,
		}
	}
	return out
}

// Raid le /proc/mdstat (legivel por qualquer usuario) e diz se algum array
// mdadm esta degradado - a diferenca entre "perdi um disco" e "perdi tudo".
func (f Fontes) Raid() []RaidArray {
	texto, ok := ler(f.P("/proc/mdstat"), 64<<10)
	if !ok {
		return []RaidArray{}
	}
	out := []RaidArray{}
	linhas := strings.Split(texto, "\n")
	for i, linha := range linhas {
		if !strings.HasPrefix(linha, "md") {
			continue
		}
		nome, _, _ := strings.Cut(linha, ":")
		a := RaidArray{Nome: strings.TrimSpace(nome), Estado: "inativo"}
		if strings.Contains(" "+linha+" ", " active ") {
			a.Estado = "ativo"
		}
		// O mapa [UU_] vem na linha seguinte: U = disco presente, _ = faltando.
		//   md0 : active raid1 sdb1[1] sda1[0]
		//         976630464 blocks super 1.2 [2/1] [U_]
		for _, seguinte := range linhas[i+1 : min(i+3, len(linhas))] {
			s := strings.TrimSpace(seguinte)
			ini := strings.LastIndex(s, "[")
			if ini < 0 || !strings.HasSuffix(s, "]") {
				continue
			}
			mapa := s[ini+1 : len(s)-1]
			// So aceita o mapa de discos; descarta "[2/1]" e "[raid1]".
			if mapa != "" && strings.Trim(mapa, "U_") == "" {
				degradado := strings.Contains(mapa, "_")
				a.Discos = mapa
				a.Degradado = &degradado
				break
			}
		}
		out = append(out, a)
	}
	return out
}

// ---------------------------------------------------------------- rede

func (f Fontes) NetBruto() map[string]AmostraNet {
	out := map[string]AmostraNet{}
	texto, _ := ler(f.P("/proc/net/dev"), maxArquivoProc)
	linhas := strings.Split(texto, "\n")
	if len(linhas) > 2 {
		linhas = linhas[2:] // duas linhas de cabecalho
	}
	for _, linha := range linhas {
		nome, resto, ok := strings.Cut(linha, ":")
		nome = strings.TrimSpace(nome)
		if !ok || nome == "" || temPrefixo(nome, f.NetIgnorar...) {
			continue
		}
		c := campos(resto)
		if len(c) < 16 {
			continue
		}
		n := make([]int64, 16)
		erro := false
		for i := range n {
			v, err := strconv.ParseInt(c[i], 10, 64)
			if err != nil {
				erro = true
				break
			}
			n[i] = v
		}
		if erro {
			continue
		}
		out[nome] = AmostraNet{
			RX: n[0], RXPkt: n[1], RXErr: n[2],
			TX: n[8], TXPkt: n[9], TXErr: n[10],
		}
	}
	return out
}

func (f Fontes) NetEstado(iface string) (up bool, mbps *int64) {
	if s, ok := ler(f.P("/sys/class/net", iface, "operstate"), 64); ok {
		up = s == "up"
	}
	// speed da erro (EINVAL) em interface virtual ou link caido; ignoramos.
	if v, ok := lerInt(f.P("/sys/class/net", iface, "speed")); ok && v > 0 {
		mbps = i64(v)
	}
	return up, mbps
}

// ---------------------------------------------------------------- sistema

func (f Fontes) SO() SO {
	campos := map[string]string{}
	texto, _ := ler(f.P("/etc/os-release"), 8192)
	for _, linha := range strings.Split(texto, "\n") {
		chave, valor, ok := strings.Cut(linha, "=")
		if !ok {
			continue
		}
		campos[chave] = strings.Trim(strings.TrimSpace(valor), `"`)
	}
	nome := campos["PRETTY_NAME"]
	if nome == "" {
		nome = campos["NAME"]
	}
	kernel, _ := ler(f.P("/proc/sys/kernel/osrelease"), 256)
	return SO{Nome: nome, ID: campos["ID"], Kernel: kernel}
}

func (f Fontes) PlacaMae() string {
	v, _ := ler(f.P("/sys/class/dmi/id/board_name"), 256)
	return v
}

// Guests conta VMs e containers lendo /etc/pve/.vmlist. Devolve nil fora do
// Proxmox, que e o esperado na maioria dos hosts.
func (f Fontes) Guests() *Guests {
	texto, ok := ler(f.P("/etc/pve/.vmlist"), maxArquivoProc)
	if !ok || texto == "" {
		return nil
	}
	var doc struct {
		IDs map[string]struct {
			Type string `json:"type"`
		} `json:"ids"`
	}
	if err := json.Unmarshal([]byte(texto), &doc); err != nil {
		return nil
	}
	g := &Guests{}
	for _, v := range doc.IDs {
		switch v.Type {
		case "qemu":
			g.Qemu++
		case "lxc":
			g.LXC++
		}
	}
	return g
}

// Extras junta tudo que os timers auxiliares deixaram em /run/sysmon/*.json.
//
// E o ponto de extensao do projeto: qualquer coleta que precise de root ou de
// executar um binario (lvs, zpool, smartctl, systemctl) roda numa unit isolada
// e deposita JSON aqui. O processo exposto a rede continua so lendo arquivo,
// sem privilegio e sem exec. Cada bloco carrega _idade_s para o cliente
// distinguir dado fresco de coletor morto - na v1, um timer parado servia o
// mesmo numero para sempre sem nenhum aviso.
func (f Fontes) Extras() map[string]Extra {
	out := map[string]Extra{}
	arquivos, _ := filepath.Glob(f.P("/run/sysmon", "*.json"))
	agora := time.Now()
	for _, arq := range arquivos {
		texto, ok := ler(arq, maxExtraBytes)
		if !ok || !json.Valid([]byte(texto)) {
			continue
		}
		e := Extra{Dados: json.RawMessage(texto)}
		if st, err := os.Stat(arq); err == nil {
			e.IdadeS = f64(arred(agora.Sub(st.ModTime()).Seconds(), 1))
		}
		nome := strings.TrimSuffix(filepath.Base(arq), ".json")
		out[nome] = e
	}
	return out
}

// Thinpools normaliza o snapshot de `lvs` que o timer gravou em extras.
func Thinpools(extras map[string]Extra) []Thinpool {
	out := []Thinpool{}
	bloco, ok := extras["thinpool"]
	if !ok {
		return out
	}
	var doc struct {
		Report []struct {
			LV []struct {
				VG   string `json:"vg_name"`
				Nome string `json:"lv_name"`
				Data string `json:"data_percent"`
				Meta string `json:"metadata_percent"`
			} `json:"lv"`
		} `json:"report"`
	}
	if err := json.Unmarshal(bloco.Dados, &doc); err != nil || len(doc.Report) == 0 {
		return out
	}
	for _, lv := range doc.Report[0].LV {
		data, e1 := strconv.ParseFloat(lv.Data, 64)
		meta, e2 := strconv.ParseFloat(lv.Meta, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		out = append(out, Thinpool{
			Nome:        lv.VG + "/" + lv.Nome,
			DataPercent: arred(data, 1),
			MetaPercent: arred(meta, 1),
			IdadeS:      bloco.IdadeS,
		})
	}
	return out
}

// ---------------------------------------------------------------- utilidades

func temPrefixo(s string, prefixos ...string) bool {
	for _, p := range prefixos {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
