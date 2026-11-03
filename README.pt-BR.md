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
- **Isolamento explícito de bibliotecas:** Recebe bibliotecas ELF via `--library-dir`, verifica seu formato e SONAME e cria wrappers quando necessário. Não baixa bibliotecas automaticamente nem garante compatibilidade ABI.
- **Normalização FHS:** Converte layouts de diretórios entre distribuições (ex: `/usr/lib64` <-> `/usr/lib/x86_64-linux-gnu` <-> `/usr/lib`) e atualiza automaticamente caminhos hardcoded em arquivos `.desktop`, `.service` e `.pc`.
- **Transpilação de Scripts via AST:** Parseia scripts de instalação (`preinst`, `postinst`, `prerm`, `postrm`) em Árvore de Sintaxe Abstrata (AST) com `mvdan.cc/sh/v3/syntax`. Converte comandos legados de serviço (`chkconfig`, `service`) para `systemctl`, adapta flags do `useradd` e remove chamadas redundantes a `ldconfig`.
- **Ponte para APKs Android:** Mantém o arquivo APK original sem reempacotar recursos, lê o `AndroidManifest.xml` e cria launchers para Waydroid com `.desktop` e ícone.
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
| **AppImage** | `.AppImage` (ELF Tipos 1 e 2) | Opcional (`--extract`) | ➔ Original + `.desktop` (padrão) |

---

## Instalação

### Instalação universal no Linux (via curl)

Compatível com Debian/Ubuntu, Fedora/RHEL, Arch, Alpine (musl) e outras distribuições Linux com `sh` POSIX, `tar`, `curl` ou `wget` e uma arquitetura Go suportada.

```sh
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh
```

Sem precisar clonar o repositório nem executar o script como root. A instalação padrão usa `~/.local/bin` para usuários comuns e `/usr/local/bin` quando já houver permissão. O script identifica a arquitetura, prioriza o binário de release (com SHA-256 validado) e, se ele ainda não existir, baixa o código-fonte e compila. Se não houver Go compatível instalado, baixa um **toolchain Go temporário oficial com verificação SHA-256**; não altera o Go do sistema. A compilação precisa de acesso à rede para baixar módulos.

```sh
# Instalação global (necessita permissões ou sudo sem prompt)
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system

# Forçar compilação de código-fonte
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --source

# Escolher diretório personalizado
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --dir "$HOME/bin"

# Exigir binário de uma release específica
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --release --version v1.0.0
```

Também funciona com `wget -qO- https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh`. **Não execute `curl | sudo sh`**: o script deve rodar como usuário normal e só requer privilégios para a cópia final quando solicitada. Enquanto não existir uma release binária, o modo padrão compila a partir da fonte. Confira o script antes de executá-lo em ambientes sensíveis.

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

### Testes Unitários e de Integração
```bash
go test -v ./...
```

### Matriz de Testes Multi-OS (VMs / Containers)
Executa a validação da instalação via `curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --system` e testa todos os 7 formatos de pacote no Debian, Ubuntu, Fedora, Arch Linux e Alpine:

```bash
# Executar a matriz em todos os 5 ambientes OS (modo local rápido)
make test-matrix-local
# ou
./tests/os_matrix/run_matrix.sh --mode local

# Executar teste de instalação online oficial via curl
./tests/os_matrix/run_matrix.sh --mode online

# Executar em uma distribuição específica (alpine, debian, ubuntu, fedora, arch)
./tests/os_matrix/run_matrix.sh --os alpine
./tests/os_matrix/run_matrix.sh --os fedora
```

---

## Licença

Distribuído sob a licença GPL-3.0. Consulte o arquivo [LICENSE](LICENSE) para mais detalhes.


## Segurança e limitações atuais

- `inspect` não executa código-fonte nem scripts.
- A compilação de tarballs requer `--allow-source-build`, Bubblewrap e user namespaces; a execução ocorre em sandbox sem rede, sem fallback para o host.
- Bibliotecas ELF ausentes exigem arquivos fornecidos pelo usuário em `--library-dir /caminho/libs`; bibliotecas sem formato ELF, SONAME ou arquitetura compatível são rejeitadas.
- Dependências entre distribuições e scripts de instalação sem conversão segura são rejeitados; o sistema não gera deliberadamente pacotes com dependências falsas.
- AppImage tipos 1 (ISO 9660) e 2 (SquashFS) podem ser convertidos com `bsdtar` ou `unsquashfs`, respectivamente. Type 0 e ZIP genérico não são suportados.
- `install --dry-run` informa o plano, sem compilar nem instalar.
- Use Go 1.26.4 ou posterior, de acordo com o `go.mod`.

## AppImage, compilação isolada e dependências entre distribuições

- **AppImage Types 1 e 2:** o padrão é preservar o arquivo original em `/opt/mutate/appimages/<nome>/<nome>.AppImage` e criar o atalho em `/usr/share/applications/<nome>.desktop`, sem executar nem extrair o AppImage. Para extrair e reconstruir, use `--extract` (Type 1 requer `bsdtar`; Type 2 requer `unsquashfs`). O ícone padrão é genérico. As exigências originais de runtime/FUSE continuam válidas.
- **Build isolado:** `--allow-source-build` exige `bubblewrap` com user namespaces. Sem sandbox a compilação não acontece; a rede fica indisponível e só o staging pode ser escrito.
- **Dependências entre distros:** `--dependency-map deps.json` recebe um objeto JSON com nomes de pacotes origem/destino e chaves `so:lib.so.N`. A tradução de scripts é conservadora; hooks sem equivalência segura geram erro.

### Estratégia de preservação dos pacotes autocontidos

- **AppImage Type 1 e Type 2:** preserva o binário original, gera `.desktop`; `--extract` permite conversão por extração.
- **APK Android:** preserva o arquivo `.apk` original e gera lançador para Waydroid, mantendo seus requisitos externos.
- **DEB/RPM/Arch/Alpine e tarballs de código-fonte:** continuam sujeitos à extração e conversão tradicional, pois não é seguro presumir que sejam autocontidos.

Exemplos:

```bash
mutate build aplicativo.AppImage --to deb
mutate build aplicativo.AppImage --to rpm --extract
mutate install aplicativo.AppImage
mutate inspect aplicativo.AppImage --extract
```

O empacotamento referencia diretamente o arquivo de origem; não realiza cópia intermediária desnecessária. A instalação cria os arquivos de destino com o gerenciador de pacotes nativo.

### Validação de pré-requisitos de execução

Ao receber um **APK Android**, os comandos `mutate inspect`, `build` e `install` verificam se `waydroid` está disponível no PATH e informam que o Waydroid precisa estar instalado e configurado para executar o aplicativo. Para **AppImage Type 2 preservado**, verificam a disponibilidade de `/dev/fuse` e `libfuse.so.2` em locais comuns, com avisos e sugestões específicas da distribuição quando ausentes. AppImages antigos podem exigir FUSE 2; runtimes mais novos podem utilizar FUSE 3. **Type 1 não exige necessariamente FUSE 2**. Com `--extract`, a extração do AppImage original não requer montagem por FUSE.

Os avisos são informativos: **não instalam dependências nem bloqueiam a geração de pacotes**. A checagem considera o host atual, não necessariamente o ambiente de destino, e não comprova que serviços ou bibliotecas estejam operacionais.

### Inspeção rápida, segurança e integração

`mutate inspect pacote.deb --fast` obtém metadados sem extrair o conteúdo nem analisar ELF (DEB, RPM, Arch, Alpine e tarballs fonte). `mutate inspect pacote.rpm --json` produz JSON estruturado. `--fast --json` retorna apenas os metadados; avisos permanecem no stderr.

AppImages chamados `exemplo-2.3.4-x86_64.AppImage` geram pacote `exemplo` com versão `2.3.4`. Use `--package-name` e `--package-version` em `build` e `install` para sobrescrever a identidade do AppImage sem extraí-lo.

A extração tem limites padrão: 250.000 entradas, 16 GiB de dados descompactados, 8 GiB por arquivo, metadados de até 4 MiB. Ajuste os limites globais com `MUTATE_MAX_EXTRACT_BYTES` e `MUTATE_MAX_EXTRACT_ENTRIES`, utilizando inteiros positivos.

Opcionalmente `MUTATE_BUILD_CACHE_DIR=/caminho/cache` monta cache offline `go-mod/` e `cargo/` **somente leitura** no Bubblewrap, mantendo a rede bloqueada.

A matriz de cinco distribuições roda **sob demanda**, reduzindo o custo do GitHub Actions. Os PRs usam um job rápido. Testes de pacotes reais são opt-in e falhas obrigatórias deixam de ser ignoradas.

Para testar os pacotes reais sob demanda, baixe primeiro as amostras e execute `RUN_REAL_PACKAGES=1 ./tests/os_matrix/run_matrix.sh --mode local`. Conversões que exigem mapeamento explícito de dependências ou ambiente de compilação isolado são registradas como **SKIP**, não **PASS**.

### Arquitetura e proteção dos arquivos originais

Os comandos `inspect`, `build` e `install` usam a mesma seleção de formato e a política `unpacker.Prepare`: preservam artefatos portáteis por padrão e aplicam `--extract` somente a AppImages. Nome e versão são validados **antes** de gerar caminhos e atalhos, tanto para AppImages preservados quanto extraídos.

O `PackageIR` separa `SourcePath` (origem) de `DestinationPath` (destino no pacote). Um arquivo original externo ao staging nunca deve ser movido ou alterado pela normalização FHS. A relocação de bibliotecas altera apenas o destino lógico no pacote, sem mover nem copiar o arquivo de origem. O **copy-on-write** é reservado aos arquivos de configuração cujo conteúdo realmente precisa ser alterado. A geração do pacote valida caminhos e consistência de todas as entradas, independentemente do formato de origem.

### Medição e comparação de desempenho

Ative `MUTATE_TIMINGS=1` para mostrar os tempos de cada fase no stderr, sem poluir a saída JSON:

```sh
MUTATE_TIMINGS=1 mutate build pacote.rpm --to deb --dependency-map mapeamento.json
MUTATE_TIMINGS=1 mutate inspect pacote.deb
```

O modo padrão não coleta essas medições. Para comparar versões em pacotes reais usando p50/p95 e execuções alternadas, utilize `benchmarks/compare.py` (instruções em `benchmarks/README.md`). A melhoria elimina I/O desnecessário na normalização FHS; não altera compressão do pacote final. Verificações de FUSE/Waydroid agora são realizadas apenas para AppImage e APK Android, e APKs preservados não passam por análise ELF de Linux.

### Desempenho da inspeção completa e preparação para interface gráfica

`mutate inspect pacote.deb` mantém análise ELF completa e consulta de provedores no sistema. Debian, Arch e Alpine passam a resolver todas as bibliotecas externas em lote, evitando varreduras repetidas das bases locais. Para analisar ELF sem consultar gerenciadores de pacotes, use `mutate inspect pacote.deb --no-resolve`: os SONAMEs continuam disponíveis e os provedores são identificados como **NOT RESOLVED**. Essa opção é útil em interfaces gráficas e ambientes offline; não substitui `--fast`.

`MUTATE_TIMINGS=1` passa a contabilizar também `cleanup`, pois remover os arquivos temporários faz parte do tempo real percebido pelo usuário. O comparador de benchmarks exibe p50/p95 por pacote, além do agregado, para identificar quais pacotes influenciaram a mudança de desempenho.

## Interface gráfica integrada (Linux)

O executável CLI Go continua compilado **sem CGO**. Quando GTK/WebKit está
disponível, o instalador também compila `mutate-gui`, uma janela **Wails
v3 nativa**, sem precisar de Chrome/Chromium. A interface HTML/CSS/JS
continua embutida no motor. Durante a compilação do código-fonte, o
instalador verifica e **instala automaticamente as bibliotecas GTK/WebKit
ausentes** com APT, DNF, pacman ou zypper, solicitando autorização sudo
somente quando necessária. Se não conseguir, a CLI continua funcionando,
e o navegador fica como alternativa.

```sh
mutate gui                      # abre janela Wails quando instalada
mutate gui --register           # cadastra o atalho no menu de aplicativos Linux
mutate gui --browser            # força alternativa pelo navegador
mutate gui --no-browser         # disponibiliza URL local sem abrir janela
```

O instalador agora **registra automaticamente** o M.U.T.A.T.E. no menu
de aplicativos Linux. Instalações de usuário criam o atalho em
`~/.local/share/applications` (ou `XDG_DATA_HOME/applications`),
e instalações de sistema usam `/usr/local/share/applications`.

```sh
curl -fsSL https://raw.githubusercontent.com/rfdetoni/mutate/main/install.sh | sh -s -- --user
```

Em servidores sem ambiente gráfico, use `--no-desktop` ou
`MUTATE_INSTALL_DESKTOP=0` para desativar o registro. Para também
**não instalar dependências gráficas**, use `--no-gui-deps` ou
`MUTATE_INSTALL_GUI_DEPS=0`. A opção `--gui-deps` força a tentativa
de instalação quando necessário. O instalador usa GTK4/WebKitGTK 6.0
quando disponível, ou GTK3/WebKit2GTK 4.1 nas distribuições compatíveis.
Não executa atualização parcial de pacotes no Arch.
O comando `mutate gui --register` continua disponível para recriar
manualmente o atalho do usuário.

A tela oferece uma área grande para arrastar o instalador ou clicar e
selecionar, detalhes verdadeiros do pacote, análise ELF, conversão para
DEB/RPM/Arch/Alpine com download, histórico da sessão e instalação
assistida. A instalação exige confirmação e autorização via **PolicyKit
(`pkexec`)** quando o processo não estiver privilegiado. É necessário
um agente polkit no desktop; a GUI nunca captura senha de administrador.
Se a instalação falhar, o pacote convertido continua disponível para baixar.

A interface usa HTTP **somente em 127.0.0.1**, com um caminho de sessão
aleatório e cabeçalho obrigatório para alterações. Arquivos ficam em
diretório temporário privado (excluído ao encerrar); upload máximo de
2 GiB. Compilação de código-fonte continua opt-in e isolada. Progresso
indica apenas fases reais, sem percentuais fictícios. Quando o shell Wails estiver instalado, a interface abre como
aplicativo desktop independente. Sem dependências GTK/WebKit de compilação,
o navegador pode ser usado como alternativa. Para uso headless, utilize
`--no-browser`.

As regras de design e comportamento estão em [DESIGN.md](DESIGN.md) e
[UX-CONTRACT.md](UX-CONTRACT.md).

### Biblioteca de aplicativos

A **Biblioteca** pesquisa os repositórios nativos da distribuição atual
(APT, DNF, pacman ou APK) e, em paralelo, as APIs públicas do **AUR**,
**Arch oficial** e **Debian Sources**. Os pacotes nativos aparecem primeiro
e podem ser instalados com confirmação e PolicyKit. Resultados remotos são
**somente para descoberta**, sem execução automática de PKGBUILDs.

A tela **Repositórios** mostra o estado de cada catálogo, permite
ativar/desativar fontes, atualizar, remover fontes adicionadas e cadastrar
novas APIs JSON HTTPS. Endereço customizado: `https://exemplo.org/search?q={query}`.
Resposta esperada: `{"results":[{"name":"app","version":"1.0","description":"...","url":"https://..."}]}`.
O cadastro persiste em `~/.config/mutate/repositories.json` ou no
`XDG_CONFIG_HOME` configurado. **Não altera** `/etc/apt`, DNF, pacman ou APK.

Ao abrir a interface, as fontes remotas são consultadas em segundo plano
e em paralelo, com indicação de atualização e falhas. A versão instalada
continua sendo a candidata definida pelo gerenciador do Linux atual;
versões de distribuições diferentes não são consideradas intercambiáveis.
