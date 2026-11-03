'use strict';

const root = document.location.pathname;
const session = root.split('/').filter(Boolean)[0];
const apiRoot = '/' + session + '/api/';
const $ = (id) => document.getElementById(id);

const app = {
  selected: null,
  job: null,
  history: [],
  busy: false,
  host: '',
  nativeTarget: '',
  view: 'convert',
  uploading: false,
  uploadPercent: null,
  requestError: '',
  polling: null,
  lastJobID: '',
  historyRenderedSignature: '',
  catalog: [],
  catalogQuery: '',
  catalogWarnings: [],
  catalogLoading: false,
  catalogError: '',
  catalogSequence: 0,
  catalogAbort: null,
  catalogPending: null,
  sources: [],
  sourcesSignature: '',
  repositoriesError: '',
  repositoriesBusy: false,
};

const viewCopy = {
  convert: ['CONVERSÃO DE PACOTES', 'Converta ou instale pacotes Linux',
    'Selecione um instalador, confira os detalhes e escolha o que fazer.'],
  inspect: ['ANÁLISE DE PACOTES', 'Inspecione um pacote',
    'Confira os metadados e analise executáveis e bibliotecas ELF.'],
  install: ['INSTALAÇÃO ASSISTIDA', 'Instale um pacote',
    'O pacote será convertido para o sistema atual antes da autorização de administrador.'],
};

async function api(path, options = {}) {
  const headers = new Headers(options.headers || {});
  if (options.method && options.method !== 'GET') {
    headers.set('X-Mutate-Session', session);
  }
  const response = await fetch(apiRoot + path, { ...options, headers, cache: 'no-store' });
  let result = null;
  try { result = await response.json(); } catch { /* malformed response */ }
  if (!response.ok) {
    throw new Error(result?.error || 'A operação não pôde ser concluída (' + response.status + ').');
  }
  return result;
}

function toast(message) {
  const element = $('toast');
  element.textContent = message;
  element.hidden = false;
  clearTimeout(toast.timer);
  toast.timer = setTimeout(() => element.hidden = true, 4500);
}

function bytes(value) {
  if (!Number.isFinite(value) || value < 0) return '—';
  if (value < 1024) return value + ' B';
  if (value < 1024 * 1024) return (value / 1024).toFixed(1) + ' KiB';
  if (value < 1024 * 1024 * 1024) return (value / 1048576).toFixed(1) + ' MiB';
  return (value / 1073741824).toFixed(2) + ' GiB';
}

function shortened(value, max = 72) {
  const str = String(value || '');
  return str.length > max ? str.slice(0, max - 1) + '…' : str;
}

function text(id, value) {
  $(id).textContent = value === undefined || value === null || value === '' ? '—' : String(value);
}

function selectedTarget() {
  return document.querySelector('input[name="target-format"]:checked')?.value || 'deb';
}

function preferredTarget() {
  return app.nativeTarget || 'deb';
}

function selectTarget(target) {
  const input = document.querySelector('input[name="target-format"][value="' + target + '"]');
  if (input) input.checked = true;
}

function setView(view) {
  app.view = view;
  document.querySelectorAll('.nav-item').forEach((button) => {
    const active = button.dataset.view === view;
    button.classList.toggle('active', active);
    if (active) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
  });
  const advanced = view === 'history' || view === 'settings' || view === 'library' || view === 'repositories';
  $('workspace-view').hidden = advanced;
  $('history-view').hidden = view !== 'history';
  $('library-view').hidden = view !== 'library';
  $('repositories-view').hidden = view !== 'repositories';
  $('settings-view').hidden = view !== 'settings';
  document.querySelector('.app-layout').classList.toggle('wide-view', advanced);
  if (!advanced) {
    const copy = viewCopy[view];
    text('view-eyebrow', copy[0]);
    text('page-title', copy[1]);
    text('page-intro', copy[2]);
    if (view === 'install' && app.nativeTarget) selectTarget(app.nativeTarget);
    refreshActions();
    $('page-title').focus?.({ preventScroll: true });
  }
  if (view === 'history') {
    renderHistory();
    $('history-title').focus({ preventScroll: true });
  }
  if (view === 'settings') $('settings-title').focus({ preventScroll: true });
  if (view === 'library') {
    renderCatalog();
    $('library-title').focus({ preventScroll: true });
    refreshRepositories();
  }
  if (view === 'repositories') {
    renderRepositories();
    $('repositories-title').focus({ preventScroll: true });
    refreshRepositories();
  }
}

function refreshActions() {
  const canUse = Boolean(app.selected) && !app.busy && !app.uploading;
  for (const id of ['inspect-btn', 'convert-btn', 'install-btn']) $(id).disabled = !canUse;
  if (!app.nativeTarget) $('install-btn').disabled = true;
  const disabledDrop = app.busy || app.uploading;
  $('drop-zone').setAttribute('aria-disabled', disabledDrop ? 'true' : 'false');
  $('drop-zone').tabIndex = disabledDrop ? -1 : 0;
  $('file-input').disabled = disabledDrop;
  for (const button of document.querySelectorAll('.catalog-install-button')) {
    button.disabled = app.busy || app.uploading;
  }
  const isInstall = app.view === 'install';
  for (const input of document.querySelectorAll('input[name="target-format"]')) {
    input.disabled = isInstall || app.busy || app.uploading;
    if (isInstall && app.nativeTarget && input.value === app.nativeTarget) input.checked = true;
  }
  text('format-help', isInstall
    ? 'A instalação utiliza ' + (app.nativeTarget || 'o formato nativo') + ' do sistema.'
    : 'Selecione o formato para gerar o pacote.');
  text('action-help', !app.selected
    ? 'Selecione um pacote para habilitar as ações.'
    : app.busy || app.uploading
      ? 'Uma operação está em andamento. Aguarde a conclusão.'
      : !app.nativeTarget
        ? 'Instalação indisponível neste sistema. Conversão e inspeção continuam disponíveis.'
        : 'Operações locais. A instalação requer confirmação explícita.');
}

function renderPackage() {
  const file = app.selected;
  const meta = file?.metadata || {};
  text('package-type', file?.format ? String(file.format).toUpperCase() : 'Nenhum');
  text('package-name', file?.file_name || 'Nenhum arquivo');
  text('package-description', file ? shortened(meta.description || 'Metadados de ' + file.file_name) : 'Selecione um pacote à esquerda');
  text('meta-name', meta.name);
  text('meta-version', meta.version ? meta.version + (meta.release ? '-' + meta.release : '') : null);
  text('meta-architecture', meta.architecture);
  text('meta-format', file?.format ? String(file.format).toUpperCase() : null);
  const required = Array.isArray(meta.dependencies) ? meta.dependencies.length : 0;
  const optional = Array.isArray(meta.recommends) ? meta.recommends.length : 0;
  text('meta-dependencies', file ? required + ' necessárias' + (optional ? ' · ' + optional + ' recomendadas' : '') : null);
  text('meta-size', file ? bytes(file.size) : null);
}

function stepLine(label, mode = 'idle') {
  const li = document.createElement('li');
  const point = document.createElement('span');
  point.className = 'step-point ' + mode;
  point.setAttribute('aria-hidden', 'true');
  li.append(point, document.createTextNode(label));
  return li;
}

function renderActivity() {
  const job = app.job;
  const list = $('activity-steps');
  list.replaceChildren();
  const barVisible = app.uploading || app.busy && job?.state === 'running';
  $('progress-track').hidden = !barVisible;
  $('status-panel').setAttribute('aria-busy', barVisible ? 'true' : 'false');
  const error = app.requestError || job?.state === 'error' && job.message || '';
  $('error-text').hidden = !error;
  $('error-text').textContent = error;
  const badge = $('status-badge');
  badge.className = 'status-badge';
  if (app.uploading) {
    text('status-title', 'Recebendo arquivo…');
    text('status-detail', app.uploadPercent !== null
      ? app.uploadPercent + '% enviado. O pacote será verificado após o recebimento.'
      : 'Copiando arquivo para uma área privada da sessão.');
    text('status-badge', 'Enviando');
    badge.classList.add('running');
    list.append(stepLine('Transferência em andamento', 'active'));
  } else if (job && job.state === 'running') {
    text('status-title', job.operation === 'catalog-install' ? 'Instalando aplicativo…' : job.operation === 'inspect' ? 'Inspecionando pacote…'
      : job.operation === 'install' ? 'Preparando instalação…' : 'Convertendo pacote…');
    text('status-detail', job.phase || 'Operação iniciada');
    text('status-badge', 'Em andamento');
    badge.classList.add('running');
    list.append(stepLine('Arquivo recebido', 'done'), stepLine(job.phase || 'Executando', 'active'));
  } else if (job && job.state === 'completed') {
    text('status-title', job.operation === 'catalog-install' ? 'Aplicativo instalado' : job.operation === 'inspect' ? 'Inspeção concluída'
      : job.operation === 'install' ? 'Instalação concluída' : 'Conversão concluída');
    text('status-detail', job.message || 'Operação concluída');
    text('status-badge', 'Concluído');
    badge.classList.add('completed');
    list.append(stepLine('Arquivo validado', 'done'), stepLine('Operação concluída', 'done'));
    if (job.operation === 'inspect') {
      list.append(stepLine(job.elf_count + ' ELF analisados', 'done'));
      const libs = Array.isArray(job.external_libraries) ? job.external_libraries : [];
      if (libs.length > 0) list.append(stepLine('Bibliotecas externas: ' + shortened(libs.join(', '), 125), 'done'));
    }
  } else if (job && job.state === 'error') {
    text('status-title', 'Operação não concluída');
    text('status-detail', 'Os arquivos originais foram preservados.');
    text('status-badge', 'Falhou');
    badge.classList.add('error');
    list.append(stepLine('Arquivo recebido', 'done'), stepLine('Verifique o erro abaixo'));
  } else if (app.selected) {
    text('status-title', 'Pacote pronto para análise');
    text('status-detail', shortened(app.selected.file_name) + ' · ' + bytes(app.selected.size));
    text('status-badge', 'Carregado');
    badge.classList.add('completed');
    list.append(stepLine('Arquivo recebido e formato reconhecido', 'done'), stepLine('Selecione uma ação', 'active'));
  } else {
    text('status-title', 'Nenhum pacote selecionado');
    text('status-detail', 'O arquivo será analisado automaticamente após a seleção.');
    text('status-badge', 'Pronto');
    list.append(stepLine('Aguardando arquivo'));
  }
  const link = $('download-link');
  const available = Boolean(job?.output_id);
  $('result-actions').hidden = !available;
  if (available) {
    link.href = apiRoot + 'download/' + encodeURIComponent(job.output_id);
    link.download = job.output_name || 'pacote';
    link.textContent = 'Baixar ' + (job.output_name || 'pacote convertido');
  }
}

function renderHistory() {
  const signature = JSON.stringify(app.history);
  if (signature === app.historyRenderedSignature) return;
  app.historyRenderedSignature = signature;
  const container = $('history-list');
  container.replaceChildren();
  if (!app.history.length) {
    const empty = document.createElement('p');
    empty.className = 'empty-note';
    empty.textContent = 'Nenhuma operação registrada nesta sessão.';
    container.append(empty);
    return;
  }
  for (const item of app.history) {
    const row = document.createElement('div');
    row.className = 'history-entry';
    const details = document.createElement('div');
    const name = document.createElement('strong');
    name.textContent = item.name;
    const label = document.createElement('small');
    const operation = { inspect: 'Inspeção', convert: 'Conversão', install: 'Instalação', 'catalog-install': 'Biblioteca · instalação' }[item.operation] || item.operation;
    label.textContent = operation + (item.target ? ' · ' + item.target.toUpperCase() : '') +
      ' · ' + (item.state === 'completed' ? 'Concluído' : 'Não concluído') +
      ' · ' + new Date(item.at).toLocaleTimeString('pt-BR', { hour: '2-digit', minute: '2-digit' });
    details.append(name, label);
    row.append(details);
    if (item.output_id) {
      const link = document.createElement('a');
      link.className = 'text-link';
      link.href = apiRoot + 'download/' + encodeURIComponent(item.output_id);
      link.download = item.output_name || 'pacote';
      link.textContent = 'Baixar';
      row.append(link);
    }
    container.append(row);
  }
}

function render() {
  renderPackage();
  renderActivity();
  if (app.view === 'history') renderHistory();
  renderCatalogStatus();
  refreshActions();

}

function applyState(state) {
  if (!state) return;
  app.selected = state.selected;
  app.job = state.job;
  app.busy = Boolean(state.busy);
  app.history = state.history || [];
  app.host = state.host || '';
  app.nativeTarget = state.native_target || '';
  render();
}

async function refreshState() {
  try {
    applyState(await api('state'));
    if (app.job?.state === 'completed' || app.job?.state === 'error') {
      if (app.lastJobID !== app.job.id) {
        app.lastJobID = app.job.id;
        toast(app.job.state === 'completed' ? 'Operação concluída' : 'Não foi possível concluir a operação');
      }
    }
    if (!app.busy && !app.uploading) stopPolling();
  } catch (error) {
    app.requestError = 'Não foi possível acessar o serviço local: ' + error.message;
    renderActivity();
    stopPolling();
  }
}
function startPolling() {
  if (app.polling) return;
  app.polling = setInterval(refreshState, 650);
}
function stopPolling() {
  if (!app.polling) return;
  clearInterval(app.polling);
  app.polling = null;
}

function uploadFile(file) {
  if (!file) return;
  if (app.busy || app.uploading) { toast('Aguarde a operação em andamento.'); return; }
  if (file.size > 2 * 1024 * 1024 * 1024) { toast('O arquivo deve ter no máximo 2 GiB.'); return; }
  app.uploading = true;
  app.uploadPercent = 0;
  app.requestError = '';
  render();
  const xhr = new XMLHttpRequest();
  xhr.open('POST', apiRoot + 'upload', true);
  xhr.setRequestHeader('X-Mutate-Session', session);
  xhr.responseType = 'json';
  xhr.upload.onprogress = (event) => {
    if (event.lengthComputable) {
      app.uploadPercent = Math.min(100, Math.round(event.loaded / event.total * 100));
      renderActivity();
    }
  };
  xhr.onload = async () => {
    app.uploading = false;
    app.uploadPercent = null;
    if (xhr.status < 200 || xhr.status >= 300) {
      app.requestError = xhr.response?.error || 'Falha ao ler o pacote selecionado.';
      render();
      return;
    }
    await refreshState();
    if (app.view === 'history' || app.view === 'settings') setView('convert');
    toast('Pacote carregado: ' + file.name);
  };
  xhr.onerror = () => {
    app.uploading = false;
    app.uploadPercent = null;
    app.requestError = 'Falha de conexão ao transferir o arquivo.';
    render();
  };
  const payload = new FormData();
  payload.append('file', file, file.name);
  xhr.send(payload);
}

function actionPayload(action) {
  return {
    action,
    target: selectedTarget(),
    resolve: $('resolve-host').checked,
    extract: $('extract-appimage').checked,
    allow_source_build: $('allow-source').checked,
    dependency_map: $('dependency-map').value.trim(),
  };
}
async function runAction(action) {
  if (!app.selected || app.busy || app.uploading) return;
  app.requestError = '';
  app.busy = true;
  render();
  try {
    await api('action', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(actionPayload(action)),
    });
    await refreshState();
    startPolling();
  } catch (error) {
    app.busy = false;
    app.requestError = error.message;
    render();
  }
}

function renderCatalogStatus() {
  const status = $('catalog-install-status');
  const job = app.job;
  if (!job || job.operation !== 'catalog-install') {
    status.hidden = true;
    return;
  }
  status.hidden = false;
  status.textContent = job.state === 'running'
    ? 'Instalando via repositórios do sistema · ' + (job.phase || 'Aguardando autorização')
    : job.state === 'completed'
      ? job.message || 'Aplicativo instalado'
      : 'Instalação não concluída: ' + (job.message || 'verifique o gerenciador de pacotes');
}

function renderCatalog() {
  const summary = $('catalog-summary');
  const results = $('catalog-results');
  const warnings = $('catalog-warnings');
  const error = $('catalog-error');
  $('catalog-search-btn').disabled = app.catalogLoading;
  $('catalog-search-btn').textContent = app.catalogLoading ? 'Pesquisando…' : 'Pesquisar';
  error.hidden = !app.catalogError;
  error.textContent = app.catalogError;
  warnings.replaceChildren();
  warnings.hidden = app.catalogWarnings.length === 0;
  for (const message of app.catalogWarnings) {
    const p = document.createElement('p');
    p.textContent = message;
    warnings.append(p);
  }
  if (app.catalogLoading) {
    summary.textContent = 'Consultando repositórios disponíveis…';
    return;
  }
  if (!app.catalogQuery) {
    summary.textContent = 'Pesquise para explorar aplicativos disponíveis.';
    results.replaceChildren();
    return;
  }
  const native = app.catalog.filter((item) => item.native).length;
  const remote = app.catalog.length - native;
  summary.textContent = app.catalog.length
    ? app.catalog.length + ' resultados para "' + app.catalogQuery + '" · ' +
      native + ' nativos · ' + remote + ' remotos'
    : 'Nenhum pacote encontrado para "' + app.catalogQuery + '". Experimente outro termo.';
  results.replaceChildren();
  for (const item of app.catalog) {
    const row = document.createElement('article');
    row.className = 'catalog-app';
    const info = document.createElement('div');
    info.className = 'catalog-app-info';
    const heading = document.createElement('div');
    heading.className = 'catalog-app-heading';
    const title = document.createElement('strong');
    title.textContent = item.name;
    const badge = document.createElement('span');
    badge.className = 'catalog-source' + (item.native ? ' native' : '');
    badge.textContent = item.native ? 'Nativo · ' + item.source : item.source;
    heading.append(title, badge);
    const description = document.createElement('p');
    description.className = 'catalog-app-description';
    description.textContent = item.description || 'Descrição não disponível.';
    const version = document.createElement('div');
    version.className = 'catalog-app-version';
    version.textContent = 'Versão: ' + (item.version || 'não informada') +
      (item.native ? ' · ' + (item.repository || 'repositórios configurados') : ' · catálogo remoto') +
      (item.out_of_date ? ' · sinalizado como desatualizado' : '');
    info.append(heading, description, version);
    const actions = document.createElement('div');
    actions.className = 'catalog-app-actions';
    if (item.installable && item.native) {
      const install = document.createElement('button');
      install.type = 'button';
      install.className = 'action-btn secondary catalog-install-button';
      install.textContent = 'Instalar';
      install.disabled = app.busy || app.uploading;
      install.addEventListener('click', () => confirmCatalog(item));
      actions.append(install);
    } else if (item.url && item.url.startsWith('https://')) {
      const link = document.createElement('a');
      link.href = item.url;
      link.target = '_blank';
      link.rel = 'noopener noreferrer';
      link.textContent = 'Ver detalhes';
      link.className = 'text-link';
      actions.append(link);
    }
    row.append(info, actions);
    results.append(row);
  }
}

async function searchCatalog(event) {
  event?.preventDefault();
  const query = $('catalog-query').value.trim();
  if (query.length < 2 || query.length > 60) {
    app.catalogError = 'Digite entre 2 e 60 caracteres para pesquisar.';
    renderCatalog();
    return;
  }
  if (app.catalogAbort) app.catalogAbort.abort();
  const request = new AbortController();
  app.catalogAbort = request;
  const current = ++app.catalogSequence;
  app.catalogLoading = true;
  app.catalogError = '';
  app.catalogQuery = query;
  renderCatalog();
  try {
    const response = await api('catalog?q=' + encodeURIComponent(query),
      { signal: request.signal });
    if (current !== app.catalogSequence) return;
    app.catalog = response.apps || [];
    app.catalogWarnings = response.warnings || [];
    app.catalogError = '';
  } catch (error) {
    if (current !== app.catalogSequence || request.signal.aborted) return;
    app.catalogError = error.message;
    app.catalog = [];
    app.catalogWarnings = [];
  } finally {
    if (current === app.catalogSequence) {
      app.catalogLoading = false;
      renderCatalog();
    }
  }
}

function confirmCatalog(item) {
  if (app.busy || app.uploading || !item.native || !item.installable) return;
  app.catalogPending = item;
  $('catalog-confirm-name').textContent = item.name +
    (item.version ? ' · versão candidata ' + item.version : '') +
    ' · ' + (item.repository || 'repositórios nativos');
  $('catalog-dialog').showModal();
  $('cancel-catalog').focus();
}

async function installCatalog() {
  const item = app.catalogPending;
  $('catalog-dialog').close();
  app.catalogPending = null;
  if (!item || app.busy) return;
  app.requestError = '';
  app.busy = true;
  render();
  try {
    await api('catalog/install', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: item.id }),
    });
    await refreshState();
    startPolling();
  } catch (error) {
    app.busy = false;
    app.catalogError = error.message;
    renderCatalog();
    refreshActions();
  }
}


function sourceStatusLabel(status) {
  return { ok:'Atualizado', updating:'Atualizando', idle:'Aguardando', error:'Falha', disabled:'Desativado' }[status] || 'Aguardando';
}
function renderRepositorySource(source) {
  const row = document.createElement('div');
  row.className = 'repo-row';
  const info = document.createElement('div');
  info.className = 'repo-info';
  const heading = document.createElement('div');
  heading.className = 'repo-title';
  const name = document.createElement('strong');
  name.textContent = source.name;
  const tag = document.createElement('span');
  tag.className = 'catalog-source';
  tag.textContent = source.builtin ? 'Incluído' : 'Adicionado';
  heading.append(name, tag);
  const description = document.createElement('div');
  description.className = 'repo-endpoint';
  description.textContent = source.endpoint;
  const status = document.createElement('div');
  status.className = 'repo-status status-' + source.status;
  status.textContent = sourceStatusLabel(source.status);
  if (source.updated_at && source.status === 'ok') {
    status.textContent += ' · ' + new Date(source.updated_at).toLocaleTimeString('pt-BR', {hour:'2-digit',minute:'2-digit'});
  }
  if (source.error) {
    status.title = source.error;
    status.textContent += ' · ' + source.error;
  }
  info.append(heading, description, status);
  const actions = document.createElement('div');
  actions.className = 'repo-controls';
  const toggle = document.createElement('input');
  toggle.type = 'checkbox';
  toggle.checked = source.enabled;
  toggle.setAttribute('aria-label', 'Ativar ' + source.name);
  toggle.addEventListener('change', async () => {
    toggle.disabled = true;
    try {
      await api('repositories/' + encodeURIComponent(source.id) + '/enabled', {
        method:'POST',headers:{'Content-Type':'application/json'},
        body:JSON.stringify({enabled:toggle.checked}),
      });
      await refreshRepositories();
    } catch (error) {
      app.repositoriesError = error.message;
      toggle.checked = source.enabled;
      renderRepositories();
    } finally { toggle.disabled = false; }
  });
  actions.append(toggle);
  if (!source.builtin) {
    const remove = document.createElement('button');
    remove.className = 'action-btn secondary repo-remove';
    remove.textContent = 'Remover';
    remove.type = 'button';
    remove.addEventListener('click', async () => {
      if (!window.confirm('Remover o catálogo "' + source.name + '"?')) return;
      remove.disabled = true;
      try {
        await api('repositories/' + encodeURIComponent(source.id), {method:'DELETE'});
        await refreshRepositories();
      } catch (error) {
        app.repositoriesError = error.message;
        renderRepositories();
        remove.disabled = false;
      }
    });
    actions.append(remove);
  }
  row.append(info, actions);
  return row;
}

function renderRepositories() {
  const box = $('repos-list');
  const errorBox = $('repos-error');
  errorBox.hidden = !app.repositoriesError;
  errorBox.textContent = app.repositoriesError;
  $('repo-add-btn').disabled = app.repositoriesBusy;
  if (app.sourcesSignature === JSON.stringify(app.sources) && box.childNodes.length) return;
  app.sourcesSignature = JSON.stringify(app.sources);
  box.replaceChildren();
  for (const source of app.sources) box.append(renderRepositorySource(source));
  const enabled = app.sources.filter(source => source.enabled);
  const updating = enabled.filter(source => source.status === 'updating').length;
  const ok = enabled.filter(source => source.status === 'ok').length;
  $('source-inline-status').textContent = updating
    ? 'Atualizando ' + updating + ' fonte(s) em segundo plano…'
    : ok + ' fonte(s) remota(s) pronta(s) · ' + enabled.length + ' habilitada(s)';
}

async function refreshRepositories() {
  try {
    const response = await api('repositories');
    const next = response.sources || [];
    const signature = JSON.stringify(next);
    if (signature !== app.sourcesSignature) {
      app.sources = next;
      app.sourcesSignature = '';
      renderRepositories();
    }
  } catch (error) {
    app.repositoriesError = error.message;
    if (app.view === 'repositories') renderRepositories();
  }
}

async function addRepository(event) {
  event.preventDefault();
  if (app.repositoriesBusy) return;
  app.repositoriesBusy = true;
  app.repositoriesError = '';
  renderRepositories();
  try {
    await api('repositories', {
      method: 'POST',
      headers: {'Content-Type':'application/json'},
      body: JSON.stringify({
        name:$('repo-name').value.trim(),
        url:$('repo-url').value.trim(),
      }),
    });
    $('repo-add-form').reset();
    await refreshRepositories();
  } catch (error) {
    app.repositoriesError = error.message;
  } finally {
    app.repositoriesBusy = false;
    renderRepositories();
  }
}

async function refreshSourceCatalogs() {
  $('repos-refresh').disabled = true;
  try {
    await api('repositories/refresh',{method:'POST'});
    await refreshRepositories();
    toast('Atualização dos catálogos iniciada.');
  } catch (error) {
    app.repositoriesError = error.message;
    renderRepositories();
  } finally { $('repos-refresh').disabled = false; }
}
$('repo-add-form').addEventListener('submit',addRepository);
$('repos-refresh').addEventListener('click',refreshSourceCatalogs);

$('catalog-form').addEventListener('submit', searchCatalog);
$('cancel-catalog').addEventListener('click', () => {
  $('catalog-dialog').close();
  app.catalogPending = null;
});
$('confirm-catalog').addEventListener('click', installCatalog);

const zone = $('drop-zone');
const input = $('file-input');
zone.addEventListener('click', () => { if (!input.disabled) input.click(); });
zone.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    if (!input.disabled) input.click();
  }
});
input.addEventListener('change', () => {
  if (input.files?.length) uploadFile(input.files[0]);
  input.value = '';
});
for (const event of ['dragenter', 'dragover']) {
  zone.addEventListener(event, (e) => {
    e.preventDefault();
    zone.classList.add('drag-over');
  });
}
for (const event of ['dragleave', 'dragend']) {
  zone.addEventListener(event, () => zone.classList.remove('drag-over'));
}
zone.addEventListener('drop', (event) => {
  event.preventDefault();
  zone.classList.remove('drag-over');
  if (event.dataTransfer?.files?.length) uploadFile(event.dataTransfer.files[0]);
});

document.querySelectorAll('.nav-item').forEach((button) => {
  button.addEventListener('click', () => setView(button.dataset.view));
});
$('inspect-btn').addEventListener('click', () => runAction('inspect'));
$('convert-btn').addEventListener('click', () => runAction('convert'));
$('install-btn').addEventListener('click', () => {
  if (!app.selected || !app.nativeTarget) return;
  $('install-dialog').showModal();
  $('cancel-install').focus();
});
$('cancel-install').addEventListener('click', () => $('install-dialog').close());
$('confirm-install').addEventListener('click', () => {
  $('install-dialog').close();
  runAction('install');
});
$('quit').addEventListener('click', async () => {
  if (app.busy || app.uploading) { toast('Aguarde a operação em andamento.'); return; }
  try {
    await api('quit', { method: 'POST' });
    toast('Sessão encerrada. Você pode fechar esta janela.');
    document.querySelectorAll('button,input').forEach((element) => element.disabled = true);
    stopPolling();
    window.close();
  } catch (error) {
    toast(error.message);
  }
});
document.addEventListener('visibilitychange', () => {
  if (!document.hidden && !app.uploading) refreshState();
});

setView('convert');
refreshState();
refreshRepositories();
setInterval(() => {
 if (!document.hidden && !app.repositoriesBusy) refreshRepositories();
}, 3000);
