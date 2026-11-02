# SYSTEM SPECIFICATION: M.U.T.A.T.E.
**Multi-format Universal Transpiler & Application Translation Engine**

## 1. Visão Geral e Objetivo
Você é um Engenheiro de Sistemas Linux Sênior e Arquiteto Go. Sua missão é construir o **M.U.T.A.T.E.** (binário CLI `mutate` e alias `mut`), um transpilador universal de pacotes Linux e adaptador de binários ELF escrito 100% em Go puro (`CGO_ENABLED=0`).

Diferente do legado `alien` (que apenas reempacota arquivos e ignora incompatibilidades binárias), o M.U.T.A.T.E. inspeciona cabeçalhos ELF, resolve dependências reversas por bibliotecas `.so` direto na distro hospedeira, isola bibliotecas incompatíveis via wrappers/RPATH, normaliza caminhos FHS, transpila scripts de instalação via AST e gera um pacote nativo válido (`.deb`, `.rpm`, `.pkg.tar.zst` ou `.apk`) sem depender de ferramentas externas de construção (`dpkg-deb`, `rpmbuild` ou `makepkg`).

---

## 2. Regras Arquiteturais Estritas (Hard Constraints)
1. **Zero CGO (`CGO_ENABLED=0`):** O binário final deve ser estático e autossuficiente. Toda extração, descompressão, leitura ELF, parsing de shell script e geração de pacote deve usar bibliotecas em Go puro.
2. **Proibido código de fachada (No Stubs/Placeholders):** Não escreva comentários como `// TODO: implement logic here`. Implemente a lógica real, tratamento de erros (`fmt.Errorf("...: %w", err)`) e testes unitários para cada módulo antes de avançar.
3. **Isolamento Seguro de Staging:** Toda extração e manipulação de arquivos deve ocorrer em diretórios temporários seguros (`os.MkdirTemp`), preservando permissões UNIX, symlinks (`os.Lstat`) e bits `setuid`/`chmod` em memória/metadados para o empacotador final.
4. **Arquitetura Modular (Clean Go Layout):** Separe claramente as responsabilidades através de interfaces Go testáveis.

---

## 3. Stack Tecnológica e Bibliotecas Obrigatórias (`go.mod`)
- **CLI Framework:** `github.com/spf13/cobra`
- **Análise ELF:** `debug/elf` (Standard Library do Go)
- **Extração RPM / CPIO:** `github.com/cavaliergopher/rpm` e `github.com/cavaliergopher/cpio`
- **Extração DEB (AR archive):** `github.com/blakesmith/ar`
- **Compressão Pura Go:**
  - `github.com/klauspost/compress/zstd` (`.zst`)
  - `github.com/ulikunitz/xz` (`.xz`)
  - `compress/gzip` e `archive/tar` / `archive/zip` (Stdlib)
- **Parsing de APK Android (AXML):** `github.com/avast/apkparser`
- **Parsing AST de Shell Script:** `mvdan.cc/sh/v3/syntax`
- **Empacotamento Final Nativo:** `github.com/goreleaser/nfpm/v2` (com drivers `deb`, `rpm`, `archlinux`, `apk`)

---

## 4. Estrutura de Diretórios do Projeto

```text
mutate/
├── cmd/
│   └── mutate/
│       └── main.go                 # Entrypoint e comandos CLI (install, build, inspect)
├── internal/
│   ├── domain/
│   │   └── package.go              # Structs centrais: PackageIR (Intermediate Representation), FileEntry, Metadata, ElfInfo
│   ├── detector/
│   │   └── magic.go                # Identificação de formatos via Magic Bytes (RPM, DEB, ZST, APK Alpine vs Android, Tarball, AppImage)
│   ├── unpacker/
│   │   ├── unpacker.go             # Interface Unpacker
│   │   ├── rpm.go                  # Extrator RPM + CPIO + metadados RPM
│   │   ├── deb.go                  # Extrator AR + control.tar + data.tar
│   │   ├── arch.go                 # Extrator .pkg.tar.zst + parser .PKGINFO
│   │   ├── alpine.go               # Extrator .apk (stream concatenado tar.gz) + .PKGINFO
│   │   ├── android.go              # Extrator .apk Android + AndroidManifest.xml + gerador de wrapper Waydroid/ATL
│   │   └── source.go               # Detector e builder isolado para tarballs (CMake, Make, Meson, Cargo)
│   ├── elfanalyzer/
│   │   ├── analyzer.go             # Varredura com debug/elf: extrai PT_INTERP, DT_NEEDED, RPATH/RUNPATH e arquitetura
│   │   └── isolation.go            # Gerador de binário wrapper ou adaptação de LD_LIBRARY_PATH / musl libc para libs isoladas
│   ├── resolver/
│   │   ├── resolver.go             # Interface DependencyResolver para a distro hospedeira
│   │   ├── debian.go               # Mapeia libfoo.so.X -> nome do pacote via dpkg -S / cache apt-file
│   │   ├── arch.go                 # Mapeia libfoo.so.X -> nome do pacote via pacman -F
│   │   ├── fedora.go               # Mapeia libfoo.so.X -> nome do pacote via dnf provides
│   │   └── bundler.go              # Fallback: baixa e embute a .so faltante em /usr/lib/<app>/ se não existir no host
│   ├── fhs/
│   │   └── normalizer.go           # Converte caminhos (/usr/lib64 <-> /usr/lib/x86_64-linux-gnu <-> /usr/lib) e corrige .desktop / .service
│   ├── scripts/
│   │   └── transpiler.go           # Usa mvdan.cc/sh/v3/syntax para analisar e traduzir scripts preinst/postinst/prerm
│   └── packager/
│       ├── nfpm_builder.go         # Gera o pacote nativo (.deb, .rpm, .pkg.tar.zst) a partir do PackageIR
│       └── installer.go            # Invoca apt, dnf ou pacman para instalar o pacote transpilado no host
└── go.mod
```

---

## 5. Especificação Detalhada dos Motores

### Motor 1: Detector Magic Bytes e Representação Intermediária (`PackageIR`)

Todo pacote de entrada deve ser convertido para uma estrutura única em memória (`PackageIR`) contendo:

* Metadados normalizados (Nome, Versão, Release, Arquitetura, Descrição, Licença).
* Árvore de arquivos extraída no diretório de staging + mapa de permissões e symlinks.
* Lista de scripts (`PreInstall`, `PostInstall`, `PreRemove`, `PostRemove`).
* O detector (`magic.go`) nunca deve confiar apenas na extensão do arquivo:
* `ED AB EE DB`: RPM
* `21 3C 61 72 63 68 3E`: DEB (AR archive)
* `28 B5 2F FD`: Zstandard (Arch `.pkg.tar.zst`)
* `1F 8B` (Gzip): Inspecionar primeiro arquivo do tar; se contiver `.PKGINFO` é APK do Alpine, caso contrário é `.tar.gz` genérico.
* `50 4B 03 04` (ZIP): Se contiver `AndroidManifest.xml`, classificar como APK Android.
* ELF com assinatura `41 49 02` no offset `0x08`: `AppImage`.

### Motor 2: Analisador ELF (`internal/elfanalyzer`)

Varre todos os arquivos extraídos no staging. Para cada arquivo que comece com os Magic Bytes `\x7fELF`:

1. Abra com `elf.NewFile()`.
2. Extraia a arquitetura (`EM_X86_64`, `EM_AARCH64`), o interpretador dinâmico (seção `.interp`, diferenciando `ld-linux` glibc de `ld-musl`) e todas as entradas `DT_NEEDED` via `f.ImportedLibraries()`.
3. Ignore bibliotecas que o próprio pacote já fornece internamente (verifique se a `.so` listada em `DT_NEEDED` já existe dentro da própria árvore extraída do pacote).
4. Retorne a lista deduplicada de bibliotecas externas `.so` exigidas pelo pacote.

### Motor 3: Resolvedor Reverso de Dependências (`internal/resolver`)

1. Detecte a distribuição hospedeira lendo `/etc/os-release`.
2. Para cada `.so` exigida (ex: `libssl.so.3`):
* **Debian/Ubuntu:** Procure primeiro localmente em `/var/lib/dpkg/info/*.list` (leitura direta ultrarrápida em Go) ou via `dpkg -S`.
* **Arch Linux:** Consulte a base local do `pacman -F` ou `/var/lib/pacman/local/`.
* **Fedora/RHEL:** Consulte via `rpm -qf` se a lib existir em `/usr/lib64` ou `dnf provides`.
3. Se uma biblioteca `.so` específica não existir na distro hospedeira (ex: versão antiga de uma lib), acione o `bundler.go`: isole a biblioteca em `/usr/lib/<app-name>/lib/` e gere um wrapper nativo em `/usr/bin/<app-name>` que define `LD_LIBRARY_PATH` antes de executar o binário real. Se o binário exigir `musl` em um host `glibc`, adicione o pacote `musl` da distro hospedeira como dependência.

### Motor 4: Normalizador FHS e Tradutor AST de Scripts (`internal/fhs` & `internal/scripts`)

1. **FHS Mapper:** Reorganize os diretórios de bibliotecas de acordo com a distro de destino (ex: movendo `/usr/lib64/*` de um RPM para `/usr/lib/x86_64-linux-gnu/` no Debian ou `/usr/lib/` no Arch). Atualize referências hardcoded dentro de arquivos `.desktop`, `.service` e `.pc`.
2. **AST Script Transpiler:** Parseie os scripts `postinst`/`prerm` usando `mvdan.cc/sh/v3/syntax`. Percorra os nós `syntax.CallExpr`:
* Substitua chamadas de `chkconfig` ou `service` por `systemctl`.
* Adapte flags específicas de `useradd`/`adduser` entre RedHat e Debian.
* Remova chamadas redundantes a `ldconfig` se o gerenciador de destino já usar triggers nativos.

### Motor 5: Empacotador Nativo (`internal/packager`)

Use a biblioteca `goreleaser/nfpm/v2` via código Go para compilar o `PackageIR` diretamente no formato nativo do sistema hospedeiro (`.deb`, `.rpm` ou `.pkg.tar.zst`), injetando a lista de dependências reais descobertas pelo Motor 3.

---

## 6. Interface de Linha de Comando (CLI)

Implemente os seguintes subcomandos:

* `mutate inspect <arquivo>`: Analisa o pacote, exibe metadados, arquitetura, interpretador ELF (`glibc`/`musl`), todas as `.so` exigidas (`DT_NEEDED`) e quais pacotes do sistema atual satisfazem cada uma.
* `mutate build <arquivo> --to <deb|rpm|arch|apk> [-o saida]`: Transpila o pacote completo para o formato escolhido sem instalar.
* `mutate install <arquivo> [--dry-run]`: Transpila o pacote para o formato nativo da máquina atual e executa a instalação real via gerenciador nativo (`apt`, `dnf`, `pacman`).

---

## 7. Plano de Execução Incremental (Siga esta ordem exata)

Não tente gerar todos os arquivos de uma só vez. Execute em fases e valide cada uma com `go build ./...` e `go test ./...`:

* **Fase 1:** Inicializar `go.mod`, criar `internal/domain/package.go` e implementar `internal/detector/magic.go` com testes unitários para cada assinatura de bytes.
* **Fase 2:** Implementar `internal/elfanalyzer` usando `debug/elf` e criar o comando `mutate inspect` funcional para arquivos locais.
* **Fase 3:** Implementar os unpackers (`rpm.go`, `deb.go`, `arch.go`, `alpine.go`) convertendo os formatos para `PackageIR`.
* **Fase 4:** Implementar `internal/resolver` (lendo `/etc/os-release` e mapeando `.so` para pacotes do host) e `internal/fhs/normalizer.go`.
* **Fase 5:** Implementar `internal/scripts/transpiler.go` com `mvdan.cc/sh/v3/syntax` e `internal/packager/nfpm_builder.go`.
* **Fase 6:** Implementar suporte a `.apk` Android (`android.go`), compilação de tarballs (`source.go`) e o comando `mutate install`.

Comece executando a **Fase 1** agora.
