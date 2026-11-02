# M.U.T.A.T.E.
**Multi-format Universal Transpiler & Application Translation Engine**

[![Linguagem](https://img.shields.io/badge/Linguagem-Go%201.26-blue.svg)](https://go.dev/)
[![CGO](https://img.shields.io/badge/CGO-Zero%20(Go%20Puro)-green.svg)](https://golang.org/)
[![Licença](https://img.shields.io/badge/Licen%C3%A7a-GPL%20v3-orange.svg)](LICENSE)
[![English Documentation](https://img.shields.io/badge/Docs-English-blue)](README.md)

Binário CLI: `mutate` (com alias rápido `mut`).

---

## Visão Geral

Diferente do legado `alien` (que apenas reempacota arquivos ignorando incompatibilidades binárias e de runtime), o **M.U.T.A.T.E.** é um transpilador universal de pacotes Linux e adaptador de binários ELF escrito 100% em Go puro (`CGO_ENABLED=0`).

O M.U.T.A.T.E. inspeciona cabeçalhos ELF, resolve dependências reversas por bibliotecas compartilhadas (`.so`) direto na distro hospedeira, isola bibliotecas incompatíveis em diretórios privados via wrappers, normaliza caminhos da hierarquia FHS, transpila scripts de instalação via AST e gera pacotes nativos válidos (`.deb`, `.rpm`, `.pkg.tar.zst` ou `.apk`) sem depender de ferramentas externas de construção (`dpkg-deb`, `rpmbuild` ou `makepkg`).

---

## Principais Recursos

- **Zero CGO (`CGO_ENABLED=0`):** Binário estático e autossuficiente, sem nenhuma dependência de bibliotecas do sistema para compilação ou execução.
- **Análise ELF Profunda:** Inspeciona binários ELF via biblioteca padrão `debug/elf`, extraindo o interpretador dinâmico (`ld-linux` glibc vs `ld-musl`), arquitetura (`x86_64`, `aarch64`), bibliotecas exigidas (`DT_NEEDED`), `RPATH` e `RUNPATH`.
- **Resolução Reversa de Dependências no Host:**
  - **Debian / Ubuntu / Mint:** Leitura direta e ultrarrápida em Go de `/var/lib/dpkg/info/*.list` + fallback para `dpkg -S`.
  - **Arch Linux / Manjaro:** Consulta aos metadados de `/var/lib/pacman/local/*/files` + fallback para `pacman -F`.
  - **Fedora / RHEL / CentOS:** Consulta local de bibliotecas em `/usr/lib64` + `rpm -qf` / `dnf provides`.
- **Isolamento Automático de Bibliotecas (Bundler):** Quando uma biblioteca `.so` não existe no sistema hospedeiro ou possui incompatibilidade de versão, o M.U.T.A.T.E. isola a `.so` em `/usr/lib/<app>/lib` e gera um script wrapper seguro que ajusta o `LD_LIBRARY_PATH`. Se o binário exigir `musl` em host `glibc`, injeta o loader musl e a dependência correspondente.
- **Normalização FHS:** Converte layouts de diretórios entre distribuições (ex: `/usr/lib64` <-> `/usr/lib/x86_64-linux-gnu` <-> `/usr/lib`) e atualiza automaticamente caminhos hardcoded em arquivos `.desktop`, `.service` e `.pc`.
- **Transpilação de Scripts via AST:** Parseia scripts de instalação (`preinst`, `postinst`, `prerm`, `postrm`) em Árvore de Sintaxe Abstrata (AST) com `mvdan.cc/sh/v3/syntax`. Converte comandos legados de serviço (`chkconfig`, `service`) para `systemctl`, adapta flags do `useradd` e remove chamadas redundantes a `ldconfig`.
- **Ponte para APKs Android:** Extrai APKs do Android, decodifica o `AndroidManifest.xml` binário (AXML) e cria launchers nativos integrados ao Waydroid / ATL com entradas `.desktop` e ícones.
- **Compilação de Tarballs Fonte:** Detecta e compila projetos em Go, Cargo, CMake, Make e Meson em ambiente de staging isolado.

---

## Formatos Suportados

| Formato | Extensão | Desempacotar | Gerar Pacote |
|---|---|:---:|:---:|
| **Debian / Ubuntu** | `.deb` | ✅ | ✅ |
| **Red Hat / Fedora** | `.rpm` | ✅ | ✅ |
| **Arch Linux** | `.pkg.tar.zst` | ✅ | ✅ |
| **Alpine Linux** | `.apk` | ✅ | ✅ |
| **Android APK** | `.apk` (com `AndroidManifest.xml`) | ✅ | ➔ Launcher Waydroid |
| **Tarball Fonte** | `.tar.gz`, `.tar.xz`, `.tar.zst` | ✅ | ➔ Pacote nativo |
| **AppImage** | `.AppImage` (ELF Tipo 2) | ✅ | ➔ Pacote nativo |

---

## Instalação

### Via Script de Instalação
```bash
git clone https://github.com/rfdetoni/mutate.git
cd mutate
./install.sh
```

### Compilando a partir do Código Fonte (Go 1.24+)
```bash
git clone https://github.com/rfdetoni/mutate.git
cd mutate
CGO_ENABLED=0 go build -ldflags="-s -w" -o mutate ./cmd/mutate
sudo install -m 0755 mutate /usr/local/bin/mutate
sudo ln -sf /usr/local/bin/mutate /usr/local/bin/mut
```

---

## Guia de Uso da CLI

### 1. Inspecionar um Pacote (`inspect`)
Analisa metadados, binários ELF, bibliotecas dinâmicas necessárias (`DT_NEEDED`) e quais pacotes do sistema atual fornecem cada uma:
```bash
mutate inspect pacote.rpm
# ou utilizando o alias mut:
mut inspect pacote.deb
```

### 2. Transpilar / Construir para Outro Formato (`build`)
Converte qualquer pacote para `.deb`, `.rpm`, `.arch` ou `.apk` sem realizar instalação:
```bash
# Converter RPM para Debian
mutate build pacote.rpm --to deb -o meupacote.deb

# Converter DEB para Arch Linux (.pkg.tar.zst)
mut build pacote.deb --to arch

# Converter DEB para Fedora RPM
mut build pacote.deb --to rpm

# Converter APK do Android para pacote Debian com launcher Waydroid
mut build app.apk --to deb
```

### 3. Instalar Diretamente no Host (`install`)
Transpila o pacote para o formato nativo da máquina atual e executa a instalação real pelo gerenciador nativo (`apt`, `dnf`, `pacman` ou `apk`):
```bash
# Simulação sem alterar o sistema (dry-run)
mutate install pacote.rpm --dry-run

# Instalação real
mutate install pacote.rpm
```

---

## Estrutura do Código-Fonte

```
mutate/
├── cmd/
│   └── mutate/                     # Ponto de entrada CLI (inspect, build, install)
├── internal/
│   ├── domain/                     # Representação Intermediária (PackageIR, Metadata, FileEntry, ElfInfo)
│   ├── detector/                   # Detecção de formatos por Magic Bytes (RPM, DEB, ZST, APK, AppImage)
│   ├── unpacker/                   # Extratores nativos (RPM, DEB, Arch, Alpine, Android, Source)
│   ├── elfanalyzer/                # Scanner ELF (debug/elf) e gerador de wrapper LD_LIBRARY_PATH
│   ├── resolver/                   # Detector de distro e resolvedor de dependências reversas .so
│   ├── fhs/                        # Normalizador FHS e atualizador de caminhos (.desktop, .service)
│   ├── scripts/                    # Transpilador AST de shell scripts (mvdan.cc/sh)
│   └── packager/                   # Construtor de pacotes nativos (nfpm/v2) e instalador
├── install.sh                      # Script de instalação POSIX
├── go.mod
└── LICENSE
```

---

## Executando os Testes

```bash
go test -v ./...
```

---

## Licença

Distribuído sob a licença GPL-3.0. Consulte o arquivo [LICENSE](LICENSE) para mais detalhes.


## Segurança e limitações atuais

- `inspect` não executa código-fonte nem scripts.
- A compilação de tarballs requer `--allow-source-build` e **não é isolada por sandbox**. Execute apenas fontes confiáveis.
- Bibliotecas ELF ausentes exigem arquivos fornecidos pelo usuário em `--library-dir /caminho/libs`; bibliotecas sem formato ELF, SONAME ou arquitetura compatível são rejeitadas.
- Dependências entre distribuições e scripts de instalação sem conversão segura são rejeitados; o sistema não gera deliberadamente pacotes com dependências falsas.
- AppImage é identificado, mas ainda não pode ser convertido. ZIP genérico também não é aceito como tarball.
- `install --dry-run` informa o plano, sem compilar nem instalar.
- Use Go 1.26.4 ou posterior, de acordo com o `go.mod`.
