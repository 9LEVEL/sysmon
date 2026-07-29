package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Os testes montam um /sys e um /proc falsos num diretorio temporario. Assim da
// para exercitar hardware que nao existe nesta maquina (AMD, RAID degradado,
// kernel sem PSI) sem precisar do host de verdade.
func fake(t *testing.T, arquivos map[string]string) Fontes {
	t.Helper()
	raiz := t.TempDir()
	for caminho, conteudo := range arquivos {
		completo := filepath.Join(raiz, caminho)
		if err := os.MkdirAll(filepath.Dir(completo), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(completo, []byte(conteudo), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return Fontes{raiz: raiz, NetIgnorar: NetIgnorarPadrao}
}

func TestTempsLeHwmon(t *testing.T) {
	f := fake(t, map[string]string{
		"/sys/class/hwmon/hwmon0/name":        "coretemp",
		"/sys/class/hwmon/hwmon0/temp1_input": "47000",
		"/sys/class/hwmon/hwmon0/temp1_label": "Package id 0",
		"/sys/class/hwmon/hwmon0/temp1_crit":  "100000",
		"/sys/class/hwmon/hwmon0/temp2_input": "45000",
		"/sys/class/hwmon/hwmon0/temp2_label": "Core 0",
	})

	temps := f.Temps()
	if len(temps) != 2 {
		t.Fatalf("esperava 2 sensores, veio %d: %+v", len(temps), temps)
	}
	if temps[0].C != 47.0 || temps[0].Label != "Package id 0" {
		t.Errorf("sensor 0 errado: %+v", temps[0])
	}
	if temps[0].Crit == nil || *temps[0].Crit != 100 {
		t.Errorf("crit deveria ser 100, veio %v", temps[0].Crit)
	}
	// temp2 nao tem _crit: precisa vir null, nao 0.
	if temps[1].Crit != nil {
		t.Errorf("crit ausente deveria ser nil, veio %v", *temps[1].Crit)
	}
}

func TestTempsFallbackThermalZone(t *testing.T) {
	// Maquina sem hwmon (VM, algumas placas OEM) precisa cair no thermal_zone.
	f := fake(t, map[string]string{
		"/sys/class/thermal/thermal_zone0/temp": "52000",
		"/sys/class/thermal/thermal_zone0/type": "acpitz",
	})
	temps := f.Temps()
	if len(temps) != 1 || temps[0].C != 52.0 || temps[0].Chip != "thermal_zone" {
		t.Fatalf("fallback nao funcionou: %+v", temps)
	}
}

func TestSensorCPUPreferePackage(t *testing.T) {
	casos := []struct {
		nome     string
		entrada  []Sensor
		esperado string
	}{
		{"intel", []Sensor{
			{Chip: "nvme", Label: "Composite", C: 60},
			{Chip: "coretemp", Label: "Core 0", C: 45},
			{Chip: "coretemp", Label: "Package id 0", C: 47},
		}, "Package id 0"},
		{"amd", []Sensor{
			{Chip: "nvme", Label: "Composite", C: 70},
			{Chip: "k10temp", Label: "Tctl", C: 55},
		}, "Tctl"},
		{"so_nvme_pega_o_mais_quente", []Sensor{
			{Chip: "nvme", Label: "Composite", C: 40},
			{Chip: "nvme", Label: "Sensor 1", C: 66},
		}, "Sensor 1"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			got := SensorCPU(c.entrada)
			if got == nil || got.Label != c.esperado {
				t.Fatalf("esperava %q, veio %+v", c.esperado, got)
			}
		})
	}
	if SensorCPU(nil) != nil {
		t.Error("lista vazia deveria devolver nil")
	}
}

func TestMemFallbackSemMemAvailable(t *testing.T) {
	// Kernel antigo ou /proc podado: sem o fallback o agente reportaria 100%
	// de RAM usada para sempre. Foi um bug real da v1.
	f := fake(t, map[string]string{
		"/proc/meminfo": "MemTotal:       8000000 kB\n" +
			"MemFree:        1000000 kB\n" +
			"Buffers:         500000 kB\n" +
			"Cached:         2500000 kB\n" +
			"SwapTotal:      2000000 kB\n" +
			"SwapFree:       1500000 kB\n",
	})
	m := f.Mem()
	if m.Percent == nil || *m.Percent != 50.0 {
		t.Fatalf("esperava 50%% usado (8G - 1G livre - 3G cache), veio %v", m.Percent)
	}
	if m.SwapPercent == nil || *m.SwapPercent != 25.0 {
		t.Errorf("swap deveria ser 25%%, veio %v", m.SwapPercent)
	}
}

func TestMemUsaMemAvailableQuandoExiste(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/meminfo": "MemTotal:       8000000 kB\n" +
			"MemFree:         100000 kB\n" +
			"MemAvailable:   6000000 kB\n" +
			"Cached:         5000000 kB\n",
	})
	m := f.Mem()
	if m.Percent == nil || *m.Percent != 25.0 {
		t.Fatalf("esperava 25%%, veio %v", m.Percent)
	}
}

func TestDescobrirMountsFiltraPseudo(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/mounts": "sysfs /sys sysfs rw 0 0\n" +
			"proc /proc proc rw 0 0\n" +
			"/dev/sda2 / ext4 rw,relatime 0 0\n" +
			"tmpfs /run tmpfs rw 0 0\n" +
			"/dev/sdb1 /mnt/backup\\040externo xfs rw 0 0\n" +
			"rpool/ROOT /rpool zfs rw 0 0\n" +
			"overlay /var/lib/docker/overlay2/x/merged overlay rw 0 0\n",
	})
	mounts := f.DescobrirMounts()
	esperado := []string{"/", "/mnt/backup externo", "/rpool"}
	if len(mounts) != len(esperado) {
		t.Fatalf("esperava %v, veio %v", esperado, mounts)
	}
	for i := range esperado {
		if mounts[i] != esperado[i] {
			t.Errorf("posicao %d: esperava %q, veio %q", i, esperado[i], mounts[i])
		}
	}
}

func TestMountsFixosDesligaDescoberta(t *testing.T) {
	f := fake(t, map[string]string{"/proc/mounts": "/dev/sda2 / ext4 rw 0 0\n"})
	f.MountsFixos = []string{"/srv"}
	if got := f.DescobrirMounts(); len(got) != 1 || got[0] != "/srv" {
		t.Fatalf("--mounts deveria vencer a descoberta, veio %v", got)
	}
}

func TestPressureAusenteDevolveNil(t *testing.T) {
	if f := fake(t, nil); f.Pressure() != nil {
		t.Error("kernel sem CONFIG_PSI deveria devolver nil, nao mapa vazio")
	}
}

func TestPressureLeAvg(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/pressure/cpu": "some avg10=1.50 avg60=0.75 avg300=0.20 total=123\n",
		"/proc/pressure/io": "some avg10=12.00 avg60=6.00 avg300=1.00 total=9\n" +
			"full avg10=8.00 avg60=4.00 avg300=0.50 total=7\n",
	})
	p := f.Pressure()
	if p["cpu"]["some_avg10"] != 1.5 {
		t.Errorf("cpu some_avg10: veio %v", p["cpu"]["some_avg10"])
	}
	if p["io"]["full_avg60"] != 4.0 {
		t.Errorf("io full_avg60: veio %v", p["io"]["full_avg60"])
	}
	if _, tem := p["cpu"]["some_avg300"]; tem {
		t.Error("avg300 nao deveria ser coletado")
	}
	if _, tem := p["memory"]; tem {
		t.Error("memory nao existe no fake, nao deveria aparecer")
	}
}

func TestRaidDetectaDegradado(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/mdstat": "Personalities : [raid1]\n" +
			"md0 : active raid1 sdb1[1] sda1[0]\n" +
			"      976630464 blocks super 1.2 [2/1] [U_]\n" +
			"      bitmap: 0/8 pages [0KB], 65536KB chunk\n" +
			"\n" +
			"md1 : active raid1 sdd1[1] sdc1[0]\n" +
			"      488254464 blocks super 1.2 [2/2] [UU]\n" +
			"\n" +
			"unused devices: <none>\n",
	})
	arrays := f.Raid()
	if len(arrays) != 2 {
		t.Fatalf("esperava 2 arrays, veio %d: %+v", len(arrays), arrays)
	}
	if arrays[0].Nome != "md0" || arrays[0].Discos != "U_" {
		t.Fatalf("md0 mal lido: %+v", arrays[0])
	}
	if arrays[0].Degradado == nil || !*arrays[0].Degradado {
		t.Error("md0 com [U_] deveria estar degradado")
	}
	if arrays[1].Degradado == nil || *arrays[1].Degradado {
		t.Error("md1 com [UU] nao deveria estar degradado")
	}
	if arrays[0].Estado != "ativo" {
		t.Errorf("estado: veio %q", arrays[0].Estado)
	}
}

func TestInfoBlocoNVMe(t *testing.T) {
	// Esta maquina de build nao tem NVMe; o sysfs falso cobre o caminho.
	f := fake(t, map[string]string{
		"/sys/block/nvme0n1/size":                  "3907029168",
		"/sys/block/nvme0n1/queue/rotational":      "0",
		"/sys/class/nvme/nvme0/model":              "Samsung SSD 990 PRO 2TB",
		"/sys/class/nvme/nvme0/hwmon3/temp1_input": "52000",
		"/sys/class/nvme/nvme0/hwmon3/temp1_label": "Composite",
	})
	b := f.InfoBloco("nvme0n1")

	if b.Tipo != "nvme" {
		t.Errorf("tipo: esperava nvme, veio %q", b.Tipo)
	}
	if b.Modelo != "Samsung SSD 990 PRO 2TB" {
		t.Errorf("modelo veio do controlador? veio %q", b.Modelo)
	}
	if b.Tamanho != 3907029168*512 {
		t.Errorf("tamanho: veio %d", b.Tamanho)
	}
	if b.TempC == nil || *b.TempC != 52.0 {
		t.Errorf("temperatura do hwmon do controlador: veio %v", b.TempC)
	}
}

func TestInfoBlocoNVMeComHwmonNoDevice(t *testing.T) {
	// Kernel mais novo pendura o hwmon no proprio device do namespace.
	f := fake(t, map[string]string{
		"/sys/block/nvme1n1/size":                      "1000215216",
		"/sys/block/nvme1n1/device/model":              "WD_BLACK SN850X 1TB",
		"/sys/block/nvme1n1/device/hwmon5/temp1_input": "44000",
	})
	b := f.InfoBloco("nvme1n1")
	if b.Modelo != "WD_BLACK SN850X 1TB" {
		t.Errorf("modelo: veio %q", b.Modelo)
	}
	if b.TempC == nil || *b.TempC != 44.0 {
		t.Errorf("temperatura: veio %v", b.TempC)
	}
}

func TestInfoBlocoSATA(t *testing.T) {
	f := fake(t, map[string]string{
		"/sys/block/sda/size":             "468862128",
		"/sys/block/sda/queue/rotational": "0",
		"/sys/block/sda/device/model":     "WDC WDS240G2G0A-  ",
		"/sys/block/sda/device/vendor":    "ATA     ",
	})
	b := f.InfoBloco("sda")
	if b.Tipo != "ssd" {
		t.Errorf("rotational=0 e nao-nvme deveria ser ssd, veio %q", b.Tipo)
	}
	if b.Modelo != "WDC WDS240G2G0A-" || b.Fabricante != "ATA" {
		t.Errorf("espacos do sysfs nao foram aparados: %q / %q", b.Modelo, b.Fabricante)
	}
	// Sem o modulo drivetemp nao ha sensor: precisa ser null, nao 0 graus.
	if b.TempC != nil {
		t.Errorf("temperatura deveria ser nil, veio %v", *b.TempC)
	}
}

func TestInfoBlocoHDD(t *testing.T) {
	f := fake(t, map[string]string{
		"/sys/block/sdb/size":                      "15628053168",
		"/sys/block/sdb/queue/rotational":          "1",
		"/sys/block/sdb/device/model":              "ST8000VN004-3CP1",
		"/sys/block/sdb/device/hwmon9/temp1_input": "38000", // drivetemp
	})
	b := f.InfoBloco("sdb")
	if b.Tipo != "hdd" {
		t.Errorf("rotational=1 deveria ser hdd, veio %q", b.Tipo)
	}
	if b.TempC == nil || *b.TempC != 38.0 {
		t.Errorf("drivetemp deveria dar temperatura, veio %v", b.TempC)
	}
}

func TestCtrlNVMe(t *testing.T) {
	casos := map[string]string{
		"nvme0n1":   "nvme0",
		"nvme10n1":  "nvme10",
		"nvme0n1p2": "nvme0",
		"sda":       "",
		"sdb1":      "",
	}
	for dev, esperado := range casos {
		if got := ctrlNVMe(dev); got != esperado {
			t.Errorf("%s: esperava %q, veio %q", dev, esperado, got)
		}
	}
}

func TestSmartNVMe(t *testing.T) {
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"nvme0n1":{
			"smart_status":{"passed":true},
			"nvme_smart_health_information_log":{
				"percentage_used":7,"available_spare":100,
				"media_errors":0,"power_on_hours":4211}}}`,
	})
	s := SmartDe(f.Extras(), "nvme0n1")
	if s == nil {
		t.Fatal("deveria ter lido o smart")
	}
	if s.Saude != "ok" {
		t.Errorf("saude: veio %q", s.Saude)
	}
	if s.DesgastePercent == nil || *s.DesgastePercent != 7 {
		t.Errorf("desgaste: veio %v", s.DesgastePercent)
	}
	if s.SpareRestante == nil || *s.SpareRestante != 100 {
		t.Errorf("spare: veio %v", s.SpareRestante)
	}
	if s.HorasLigado == nil || *s.HorasLigado != 4211 {
		t.Errorf("horas: veio %v", s.HorasLigado)
	}
}

func TestSmartSATATraduzAtributos(t *testing.T) {
	// SATA reporta desgaste como vida RESTANTE (100 a 0) num atributo cujo id
	// varia por fabricante; o desgaste e o complemento.
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{
			"smart_status":{"passed":false},
			"power_on_time":{"hours":18320},
			"ata_smart_attributes":{"table":[
				{"id":5,"name":"Reallocated_Sector_Ct","value":100,"raw":{"value":12}},
				{"id":233,"name":"Media_Wearout_Indicator","value":88,"raw":{"value":88}}]}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s == nil {
		t.Fatal("deveria ter lido o smart")
	}
	if s.Saude != "falha" {
		t.Errorf("passed:false deveria virar falha, veio %q", s.Saude)
	}
	if s.Realocados == nil || *s.Realocados != 12 {
		t.Errorf("realocados: veio %v", s.Realocados)
	}
	if s.HorasLigado == nil || *s.HorasLigado != 18320 {
		t.Errorf("horas: veio %v", s.HorasLigado)
	}
	if s.DesgastePercent == nil || *s.DesgastePercent != 12 {
		t.Errorf("value 88 = 12%% consumido, veio %v", s.DesgastePercent)
	}
}

func TestSmartAusente(t *testing.T) {
	// Sem smartmontools instalado o campo tem que ser nil, nao zeros.
	if s := SmartDe(map[string]Extra{}, "sda"); s != nil {
		t.Errorf("sem extras deveria ser nil, veio %+v", s)
	}
	f := fake(t, map[string]string{"/run/sysmon/smart.json": `{"sda":{}}`})
	if s := SmartDe(f.Extras(), "nvme9n1"); s != nil {
		t.Errorf("disco fora do snapshot deveria ser nil, veio %+v", s)
	}
}

func TestSmartCarregaIdade(t *testing.T) {
	f := fake(t, map[string]string{
		"/run/sysmon/smart.json": `{"sda":{"smart_status":{"passed":true}}}`,
	})
	s := SmartDe(f.Extras(), "sda")
	if s == nil || s.IdadeS == nil {
		t.Fatal("idade do snapshot deveria ser propagada para o cliente")
	}
}

func TestRaidAusente(t *testing.T) {
	if got := fake(t, nil).Raid(); len(got) != 0 {
		t.Errorf("sem /proc/mdstat deveria vir lista vazia, veio %+v", got)
	}
}

func TestNetBrutoIgnoraVirtuais(t *testing.T) {
	linha := func(nome string, rx, tx int64) string {
		return "  " + nome + ": " +
			itoa(rx) + " 10 1 0 0 0 0 0 " + itoa(tx) + " 20 2 0 0 0 0 0\n"
	}
	f := fake(t, map[string]string{
		"/proc/net/dev": "Inter-|   Receive       |  Transmit\n" +
			" face |bytes packets errs |bytes packets errs\n" +
			linha("lo", 999, 999) +
			linha("eno1", 1000, 2000) +
			linha("vmbr0", 3000, 4000) +
			linha("veth1a2b", 5, 5) +
			linha("fwbr101i0", 5, 5),
	})
	n := f.NetBruto()
	if len(n) != 2 {
		t.Fatalf("esperava eno1 e vmbr0, veio %v", chaves(n))
	}
	if n["eno1"].RX != 1000 || n["eno1"].TX != 2000 {
		t.Errorf("eno1 mal lido: %+v", n["eno1"])
	}
	if n["eno1"].RXErr != 1 || n["eno1"].TXErr != 2 {
		t.Errorf("erros mal lidos: %+v", n["eno1"])
	}
}

func TestTaxaToleraContadorQueZerou(t *testing.T) {
	if got := taxa(500, 1000, true, 5); got != nil {
		t.Errorf("contador que voltou deveria dar nil, veio %v", *got)
	}
	if got := taxa(1000, 0, false, 5); got != nil {
		t.Error("sem amostra anterior deveria dar nil")
	}
	if got := taxa(1000, 500, true, 0); got != nil {
		t.Error("dt zero deveria dar nil, nao divisao por zero")
	}
	got := taxa(2000, 1000, true, 4)
	if got == nil || *got != 250 {
		t.Errorf("esperava 250 B/s, veio %v", got)
	}
}

func TestExtrasCarregaIdadeEThinpool(t *testing.T) {
	f := fake(t, map[string]string{
		"/run/sysmon/thinpool.json": `{"report":[{"lv":[
			{"vg_name":"pve","lv_name":"data","data_percent":"62.40","metadata_percent":"3.10"}
		]}]}`,
		"/run/sysmon/quebrado.json": "isso nao e json",
		"/run/sysmon/custom.json":   `{"qualquer":"coisa"}`,
	})

	e := f.Extras()
	if _, tem := e["quebrado"]; tem {
		t.Error("json invalido deveria ser descartado")
	}
	if _, tem := e["custom"]; !tem {
		t.Error("bloco arbitrario deveria passar sem interpretacao")
	}
	if e["thinpool"].IdadeS == nil {
		t.Fatal("idade deveria ser preenchida a partir do mtime")
	}

	tp := Thinpools(e)
	if len(tp) != 1 || tp[0].Nome != "pve/data" || tp[0].DataPercent != 62.4 {
		t.Fatalf("thinpool mal normalizado: %+v", tp)
	}
	if tp[0].MetaPercent != 3.1 {
		t.Errorf("meta: veio %v", tp[0].MetaPercent)
	}
}

func TestExtrasIdadeCresceComTimerParado(t *testing.T) {
	// O sintoma que a v1 nao detectava: timer morto servindo dado congelado.
	f := fake(t, map[string]string{"/run/sysmon/thinpool.json": `{"report":[]}`})
	antigo := time.Now().Add(-2 * time.Hour)
	arq := f.P("/run/sysmon/thinpool.json")
	if err := os.Chtimes(arq, antigo, antigo); err != nil {
		t.Fatal(err)
	}
	idade := f.Extras()["thinpool"].IdadeS
	if idade == nil || *idade < 7000 {
		t.Fatalf("idade deveria refletir 2h, veio %v", idade)
	}
}

func TestGuestsForaDoProxmox(t *testing.T) {
	if fake(t, nil).Guests() != nil {
		t.Error("host sem /etc/pve deveria devolver nil")
	}
}

func TestGuestsConta(t *testing.T) {
	f := fake(t, map[string]string{
		"/etc/pve/.vmlist": `{"ids":{
			"100":{"type":"qemu"},"101":{"type":"qemu"},"200":{"type":"lxc"}}}`,
	})
	g := f.Guests()
	if g == nil || g.Qemu != 2 || g.LXC != 1 {
		t.Fatalf("contagem errada: %+v", g)
	}
}

func TestCPUBrutoSoma(t *testing.T) {
	f := fake(t, map[string]string{
		"/proc/stat": "cpu  100 20 30 800 50 0 10 0 0 0\ncpu0 50 10 15 400 25 0 5 0 0 0\n",
	})
	idle, total, ok := f.CPUBruto()
	if !ok {
		t.Fatal("deveria ter lido")
	}
	if idle != 850 { // idle 800 + iowait 50
		t.Errorf("idle: esperava 850, veio %d", idle)
	}
	if total != 1010 {
		t.Errorf("total: esperava 1010, veio %d", total)
	}
}

func TestSOLeOsRelease(t *testing.T) {
	f := fake(t, map[string]string{
		"/etc/os-release":            "PRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\nID=debian\n",
		"/proc/sys/kernel/osrelease": "6.8.12-4-pve\n",
	})
	so := f.SO()
	if so.Nome != "Debian GNU/Linux 12 (bookworm)" || so.ID != "debian" {
		t.Errorf("os-release mal lido: %+v", so)
	}
	if so.Kernel != "6.8.12-4-pve" {
		t.Errorf("kernel: veio %q", so.Kernel)
	}
}

// -------------------------------------------------------------- utilidades

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}

func chaves[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
