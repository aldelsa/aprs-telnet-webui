package main

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "html/template"
    "log"
    "net"
    "net/http"
    "os"
    "regexp"
    "strconv"
    "strings"
    "sync"
    "time"
)

const (
    defaultTimeoutSec = 6
    maxMsgLen         = 200
)

var (
    telnetHost string
    telnetPort int
    destRegex  = regexp.MustCompile(`^[A-Z0-9]+([-/][A-Z0-9]+)?$`)
    sessions   = make(map[string]*TelnetSession)
    sessMutex  = sync.Mutex{}
)

type TelnetSession struct {
    Conn   net.Conn
    Reader *bufio.Reader
    Mutex  sync.Mutex
     BannerShown bool
}

type loginReq struct {
    Callsign string `json:"callsign"`
}

type sendReq struct {
    Callsign   string `json:"callsign"`
    Dest       string `json:"dest"`
    Msg        string `json:"msg"`
    SessionKey string `json:"session_key,omitempty"`
}

type respPayload struct {
    Logged     bool   `json:"logged,omitempty"`
    Response   string `json:"response,omitempty"`
    Error      string `json:"error,omitempty"`
    Len        int    `json:"len,omitempty"`
    SessionKey string `json:"session_key,omitempty"`
}

var indexTmpl *template.Template

func main() {
    telnetHost = os.Getenv("TELNET_HOST")
    portStr := os.Getenv("TELNET_PORT")
    if telnetHost == "" || portStr == "" {
        log.Fatal("ERROR: Debes definir TELNET_HOST y TELNET_PORT")
    }
    p, err := strconv.Atoi(portStr)
    if err != nil {
        log.Fatalf("TELNET_PORT inválido: %v", err)
    }
    telnetPort = p

    indexTmpl, err = template.ParseFiles("templates/index.html")
    if err != nil {
        log.Fatalf("Error cargando template: %v", err)
    }

    mux := http.NewServeMux()
    mux.HandleFunc("/", indexHandler)
    mux.HandleFunc("/login", loginHandler)
    mux.HandleFunc("/send", sendHandler)

    addr := ":8080"
    srv := &http.Server{
        Addr:         addr,
        Handler:      loggingMiddleware(mux),
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 60 * time.Second,
    }

    log.Printf("Servidor web en http://localhost%s ...", addr)
    if err := srv.ListenAndServe(); err != nil {
        log.Fatalf("server error: %v", err)
    }
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        http.Error(w, "only GET allowed", http.StatusMethodNotAllowed)
        return
    }
    _ = indexTmpl.Execute(w, map[string]any{"Host": telnetHost, "Port": telnetPort})
}

// LOGIN: crea la sesión persistente
func loginHandler(w http.ResponseWriter, r *http.Request) {
    var req loginReq
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, respPayload{Error: "JSON inválido"}, http.StatusBadRequest)
        return
    }
    req.Callsign = strings.ToUpper(strings.TrimSpace(req.Callsign))
    if req.Callsign == "" {
        writeJSON(w, respPayload{Error: "callsign obligatorio"}, http.StatusBadRequest)
        return
    }

    base := normalizeCallsign(req.Callsign)
    pass := APRSPasscode(base)

    ctx, cancel := context.WithTimeout(r.Context(), defaultTimeoutSec*time.Second)
    defer cancel()

    addr := fmt.Sprintf("%s:%d", telnetHost, telnetPort)
    conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
    if err != nil {
        writeJSON(w, respPayload{Error: err.Error()}, http.StatusBadGateway)
        return
    }

    reader := bufio.NewReader(conn)
    loginCmd := fmt.Sprintf("user %s pass %d vers APRSClient 1.0\r\n", base, pass)
    if _, err := conn.Write([]byte(loginCmd)); err != nil {
        conn.Close()
        writeJSON(w, respPayload{Error: err.Error()}, http.StatusBadGateway)
        return
    }

    var sb strings.Builder
    logged := false
    for i := 0; i < 10; i++ {
        conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
        line, err := reader.ReadString('\n')
        if len(line) > 0 {
            sb.WriteString(line)
            if strings.HasPrefix(line, "# logresp ") && strings.Contains(strings.ToLower(line), "verified") {
                logged = true
                break
            }
        }
        if err != nil {
            if ne, ok := err.(net.Error); ok && ne.Timeout() {
                continue
            }
            conn.Close()
            writeJSON(w, respPayload{Error: err.Error(), Response: sb.String()}, http.StatusBadGateway)
            return
        }
    }

    if !logged {
        conn.Close()
        writeJSON(w, respPayload{Error: "Login no verificado", Response: sb.String()}, http.StatusBadGateway)
        return
    }

    // Guardar sesión persistente
    sessMutex.Lock()
    sessions[base] = &TelnetSession{
        Conn:   conn,
        Reader: reader,
    }
    sessMutex.Unlock()

    log.Printf("Login correcto: %s", base)
    writeJSON(w, respPayload{Logged: true, Response: sb.String(), SessionKey: base}, http.StatusOK)
}

// padDest devuelve el destinatario en mayúsculas y con exactamente 9 caracteres
func padDest(dest string) string {
    dest = strings.ToUpper(strings.TrimSpace(dest))
    if len(dest) > 9 {
        return dest[:9]
    }
    return dest + strings.Repeat(" ", 9-len(dest))
}

func sendHandler(w http.ResponseWriter, r *http.Request) {
    var req sendReq
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSON(w, respPayload{Error: "JSON inválido"}, http.StatusBadRequest)
        return
    }
    req.Callsign = strings.ToUpper(strings.TrimSpace(req.Callsign))
    req.Dest = strings.ToUpper(strings.TrimSpace(req.Dest))
    req.Msg = strings.TrimSpace(req.Msg)

    if req.Callsign == "" || req.Dest == "" || req.Msg == "" {
        writeJSON(w, respPayload{Error: "todos los campos son obligatorios"}, http.StatusBadRequest)
        return
    }
    if len(req.Msg) > maxMsgLen {
        writeJSON(w, respPayload{Error: fmt.Sprintf("mensaje demasiado largo (máx %d)", maxMsgLen)}, http.StatusBadRequest)
        return
    }
    if !destRegex.MatchString(req.Dest) {
        writeJSON(w, respPayload{Error: "destinatario inválido"}, http.StatusBadRequest)
        return
    }

    sessMutex.Lock()
    session, ok := sessions[normalizeCallsign(req.Callsign)]
    sessMutex.Unlock()
    if !ok || session == nil {
        writeJSON(w, respPayload{Error: "no hay sesión activa, haz login primero"}, http.StatusBadRequest)
        return
    }

    session.Mutex.Lock()
    defer session.Mutex.Unlock()

    // aplicamos padding aquí en el backend
    destPadded := padDest(req.Dest)

    // construimos el paquete APRS-IS válido
    packet := fmt.Sprintf("%s>APRS,TCPIP::%s:%s\r\n",
        normalizeCallsign(req.Callsign),
        destPadded,
        req.Msg)

    if _, err := session.Conn.Write([]byte(packet)); err != nil {
        writeJSON(w, respPayload{Error: err.Error()}, http.StatusBadGateway)
        return
    }

    var sb strings.Builder
    sb.WriteString(">> SENT: " + strings.TrimSpace(packet) + "\n")

    // leer eco o respuesta breve del servidor
    for i := 0; i < 10; i++ {
      session.Conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
      line, err := session.Reader.ReadString('\n')
      if len(line) > 0 {
          trimmed := strings.TrimSpace(line)
          // filtrar banners solo si ya se mostró
          if strings.HasPrefix(trimmed, "# aprsc ") {
              if !session.BannerShown {
                  sb.WriteString(line)          // mostramos solo la primera vez
                  session.BannerShown = true    // marcamos que ya se mostró
              }
              continue // ignorar banner futuras líneas
          }

          sb.WriteString(line) // resto de líneas normales
      }

      if err != nil {
          if ne, ok := err.(net.Error); ok && ne.Timeout() {
              continue
          }
          break
      }
    }

    log.Printf("Mensaje: %s -> %s : %s", req.Callsign, req.Dest, req.Msg)
    writeJSON(w, respPayload{Response: sb.String(), Len: len(sb.String())}, http.StatusOK)
}

func normalizeCallsign(cs string) string {
    cs = strings.ToUpper(strings.TrimSpace(cs))
    if idx := strings.Index(cs, "-"); idx != -1 {
        cs = cs[:idx]
    }
    return cs
}

func APRSPasscode(callsign string) int {
    cs := strings.ToUpper(strings.TrimSpace(callsign))
    hash := 0x73e2
    for i, r := range cs {
        if i%2 == 0 {
            hash ^= int(r) << 8
        } else {
            hash ^= int(r)
        }
    }
    return hash & 0x7fff
}

func writeJSON(w http.ResponseWriter, p respPayload, status int) {
    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(p)
}

func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
    })
}
