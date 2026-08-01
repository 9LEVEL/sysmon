//go:build windows

package bandeja

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

// Bandeja do Windows em Win32 puro.
//
// Sao tres pecas que precisam existir juntas:
//
//  1. uma janela escondida, porque o Shell_NotifyIcon entrega clique e menu
//     como MENSAGEM de janela - sem janela nao ha para onde mandar;
//  2. um laco de mensagens proprio, preso a uma thread do sistema, porque
//     mensagem de janela no Win32 so chega na thread que a criou;
//  3. um icone gerado em tempo de execucao, porque a cor muda com a
//     severidade e nao da para embutir um .ico por nivel.
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassEx  = user32.NewProc("RegisterClassExW")
	pCreateWindowEx   = user32.NewProc("CreateWindowExW")
	pDefWindowProc    = user32.NewProc("DefWindowProcW")
	pGetMessage       = user32.NewProc("GetMessageW")
	pTranslateMessage = user32.NewProc("TranslateMessage")
	pDispatchMessage  = user32.NewProc("DispatchMessageW")
	pPostMessage      = user32.NewProc("PostMessageW")
	pPostQuitMessage  = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu  = user32.NewProc("CreatePopupMenu")
	pAppendMenu       = user32.NewProc("AppendMenuW")
	pDestroyMenu      = user32.NewProc("DestroyMenu")
	pTrackPopupMenu   = user32.NewProc("TrackPopupMenu")
	pGetCursorPos     = user32.NewProc("GetCursorPos")
	pSetForegroundWin = user32.NewProc("SetForegroundWindow")
	pCreateIcon       = user32.NewProc("CreateIconIndirect")
	pDestroyIcon      = user32.NewProc("DestroyIcon")

	pShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")

	pCreateBitmap     = gdi32.NewProc("CreateBitmap")
	pCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	pDeleteObject     = gdi32.NewProc("DeleteObject")

	pGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

const (
	nimAdd    = 0x0
	nimModify = 0x1
	nimDelete = 0x2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4
	nifInfo    = 0x10

	wmDestroy      = 0x0002
	wmCommand      = 0x0111
	wmRButtonUp    = 0x0205
	wmLButtonUp    = 0x0202
	wmLButtonDbl   = 0x0203
	wmBandeja      = 0x0400 + 1 // WM_APP + 1
	wsOverlapped   = 0x00000000
	hwndMessage    = ^uintptr(2) // (HWND)-3
	tpmRightButton = 0x0002
	mfString       = 0x0000
	mfSeparator    = 0x0800
	mfChecked      = 0x0008

	idMostrar   = 1
	idAtualizar = 2
	idTopo      = 3
	idSair      = 4
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type notifyIconData struct {
	cbSize           uint32
	hWnd             syscall.Handle
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            syscall.Handle
	szTip            [128]uint16
	dwState          uint32
	dwStateMask      uint32
	szInfo           [256]uint16
	uVersion         uint32
	szInfoTitle      [64]uint16
	dwInfoFlags      uint32
	guidItem         [16]byte
	hBalloonIcon     syscall.Handle
}

type msg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

type ponto struct{ x, y int32 }

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  syscall.Handle
	hbmColor syscall.Handle
}

type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

type bandejaWin struct {
	hwnd   syscall.Handle
	acoes  Acoes
	mu     sync.Mutex
	icone  syscall.Handle
	nivel  int
	dica   string
	fechou bool
}

// instancia e global porque o wndProc do Win32 e uma funcao C: ela recebe
// so o handle da janela, sem lugar para carregar um ponteiro nosso. Ha uma
// bandeja por processo, entao a global e honesta aqui.
var instancia *bandejaWin

// Iniciar cria o icone e sobe o laco de mensagens numa goroutine propria.
func Iniciar(acoes Acoes) (Bandeja, error) {
	b := &bandejaWin{acoes: acoes, nivel: -1}
	pronto := make(chan error, 1)
	go b.laco(pronto)
	if err := <-pronto; err != nil {
		return nil, err
	}
	instancia = b
	return b, nil
}

func (b *bandejaWin) laco(pronto chan<- error) {
	// Mensagem de janela so chega na thread que criou a janela: sem prender
	// a goroutine a uma thread do sistema, o laco perderia mensagens assim
	// que o escalonador do Go a movesse.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	classe, _ := syscall.UTF16PtrFromString("sysmon_bandeja")
	hinst, _, _ := pGetModuleHandle.Call(0)

	wc := wndClassEx{
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     syscall.Handle(hinst),
		lpszClassName: classe,
	}
	wc.cbSize = uint32(unsafe.Sizeof(wc))
	if atom, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 {
		pronto <- errors.New("nao consegui registrar a janela da bandeja: " + err.Error())
		return
	}

	// Janela so de mensagem (HWND_MESSAGE): nunca aparece na tela nem na
	// barra de tarefas, existe apenas para receber os cliques do icone.
	hwnd, _, err := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(classe)),
		uintptr(unsafe.Pointer(classe)), wsOverlapped, 0, 0, 0, 0,
		hwndMessage, 0, hinst, 0)
	if hwnd == 0 {
		pronto <- errors.New("nao consegui criar a janela da bandeja: " + err.Error())
		return
	}
	b.hwnd = syscall.Handle(hwnd)

	if err := b.adicionar(); err != nil {
		pronto <- err
		return
	}
	pronto <- nil

	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = erro
			return
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (b *bandejaWin) dados(flags uint32) *notifyIconData {
	d := &notifyIconData{
		hWnd: b.hwnd, uID: 1, uFlags: flags,
		uCallbackMessage: wmBandeja, hIcon: b.icone,
	}
	d.cbSize = uint32(unsafe.Sizeof(*d))
	copiar(d.szTip[:], b.dica)
	return d
}

func (b *bandejaWin) adicionar() error {
	b.icone = criarIcone(Cor(0))
	b.dica = "sysmon"
	d := b.dados(nifMessage | nifIcon | nifTip)
	if r, _, err := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(d))); r == 0 {
		return errors.New("Shell_NotifyIcon recusou o icone: " + err.Error())
	}
	return nil
}

// Estado repinta o icone. O HICON antigo e destruido: o Windows nao os
// recolhe sozinho, e um icone por coleta vazaria handle ate o processo cair.
func (b *bandejaWin) Estado(nivel int, dica string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fechou || (nivel == b.nivel && dica == b.dica) {
		return
	}
	antigo := b.icone
	b.icone = criarIcone(Cor(nivel))
	b.nivel, b.dica = nivel, dica
	d := b.dados(nifIcon | nifTip)
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(d)))
	if antigo != 0 {
		pDestroyIcon.Call(uintptr(antigo))
	}
}

func (b *bandejaWin) Notificar(titulo, texto string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fechou {
		return
	}
	d := b.dados(nifInfo)
	copiar(d.szInfoTitle[:], titulo)
	copiar(d.szInfo[:], texto)
	d.dwInfoFlags = 0
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func (b *bandejaWin) Fechar() {
	b.mu.Lock()
	if b.fechou {
		b.mu.Unlock()
		return
	}
	b.fechou = true
	d := b.dados(0)
	b.mu.Unlock()
	pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(d)))
	if b.icone != 0 {
		pDestroyIcon.Call(uintptr(b.icone))
	}
	pPostMessage.Call(uintptr(b.hwnd), wmDestroy, 0, 0)
}

// wndProc recebe os cliques do icone.
func wndProc(hwnd syscall.Handle, m uint32, wp, lp uintptr) uintptr {
	b := instancia
	switch m {
	case wmBandeja:
		switch uint32(lp) {
		case wmLButtonUp, wmLButtonDbl:
			chamar(b, func(a Acoes) func() { return a.Mostrar })
		case wmRButtonUp:
			menu(b)
		}
		return 0
	case wmCommand:
		if b == nil {
			return 0
		}
		switch uint32(wp) & 0xffff {
		case idMostrar:
			chamar(b, func(a Acoes) func() { return a.Mostrar })
		case idAtualizar:
			chamar(b, func(a Acoes) func() { return a.Atualizar })
		case idTopo:
			chamar(b, func(a Acoes) func() { return a.Topo })
		case idSair:
			chamar(b, func(a Acoes) func() { return a.Sair })
		}
		return 0
	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(uintptr(hwnd), uintptr(m), wp, lp)
	return r
}

func chamar(b *bandejaWin, sel func(Acoes) func()) {
	if b == nil {
		return
	}
	if f := sel(b.acoes); f != nil {
		go f() // nao segura o laco de mensagens
	}
}

func menu(b *bandejaWin) {
	if b == nil {
		return
	}
	h, _, _ := pCreatePopupMenu.Call()
	if h == 0 {
		return
	}
	defer pDestroyMenu.Call(h)

	item := func(id uintptr, texto string, marcado bool) {
		p, _ := syscall.UTF16PtrFromString(texto)
		flags := uintptr(mfString)
		if marcado {
			flags |= mfChecked
		}
		pAppendMenu.Call(h, flags, id, uintptr(unsafe.Pointer(p)))
	}
	item(idMostrar, "Mostrar", false)
	item(idAtualizar, "Atualizar agora", false)
	noTopo := false
	if b.acoes.NoTopo != nil {
		noTopo = b.acoes.NoTopo()
	}
	item(idTopo, "Sempre no topo", noTopo)
	pAppendMenu.Call(h, mfSeparator, 0, 0)
	item(idSair, "Sair", false)

	var p ponto
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	// Sem trazer a janela para a frente, o menu fica aberto e nao fecha ao
	// clicar fora - comportamento documentado do TrackPopupMenu.
	pSetForegroundWin.Call(uintptr(b.hwnd))
	pTrackPopupMenu.Call(h, tpmRightButton, uintptr(p.x), uintptr(p.y), 0,
		uintptr(b.hwnd), 0)
	pPostMessage.Call(uintptr(b.hwnd), 0, 0, 0)
}

// criarIcone gera um HICON quadrado da cor pedida.
//
// Um .ico por severidade seria quatro arquivos embutidos e nenhum ganho: a
// forma e a mesma, so a cor muda. Gerar em tempo de execucao tambem deixa a
// paleta viver num lugar so.
func criarIcone(r, g, bl byte) syscall.Handle {
	const lado = 16
	bi := bitmapInfoHeader{
		biWidth: lado, biHeight: lado, biPlanes: 1, biBitCount: 32,
	}
	bi.biSize = uint32(unsafe.Sizeof(bi))

	var pixels unsafe.Pointer
	hbm, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0,
		uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if hbm == 0 {
		return 0
	}
	// BGRA, com alfa cheio: canto arredondado seria bonito e some no
	// tamanho de 16px que a barra de tarefas usa.
	buf := unsafe.Slice((*byte)(pixels), lado*lado*4)
	for i := 0; i < lado*lado; i++ {
		buf[i*4+0] = bl
		buf[i*4+1] = g
		buf[i*4+2] = r
		buf[i*4+3] = 0xff
	}

	// Mascara zerada: com o icone em 32 bits quem manda e o alfa, mas o
	// CreateIconIndirect exige a mascara mesmo assim.
	hmask, _, _ := pCreateBitmap.Call(lado, lado, 1, 1, 0)

	ii := iconInfo{fIcon: 1, hbmMask: syscall.Handle(hmask),
		hbmColor: syscall.Handle(hbm)}
	h, _, _ := pCreateIcon.Call(uintptr(unsafe.Pointer(&ii)))

	// Os bitmaps ja foram copiados para dentro do icone.
	pDeleteObject.Call(hbm)
	pDeleteObject.Call(hmask)
	return syscall.Handle(h)
}

func copiar(dst []uint16, s string) {
	u, err := syscall.UTF16FromString(s)
	if err != nil {
		return
	}
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

// Disponivel diz se ha bandeja de verdade nesta plataforma. Ver a versao
// para os demais sistemas.
func Disponivel() bool { return true }
