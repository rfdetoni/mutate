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
  const advanced = view === 'history' || view === 'settings';
  $('workspace-view').hidden = advanced;
  $('history-view').hidden = view !== 'history';
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
  if (view === 'history') renderHistory();
}

function refreshActions() {
  const canUse = Boolean(app.selected) && !app.busy && !app.uploading;
  for (const id of ['inspect-btn', 'convert-btn', 'install-btn']) $(id).disabled = !canUse;
  if (!app.nativeTarget) $('install-btn').disabled = true;
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
    text('status-title', job.operation === 'inspect' ? 'Inspecionando pacote…'
      : job.operation === 'install' ? 'Preparando instalação…' : 'Convertendo pacote…');
    text('status-detail', job.phase || 'Operação iniciada');
    text('status-badge', 'Em andamento');
    badge.classList.add('running');
    list.append(stepLine('Arquivo recebido', 'done'), stepLine(job.phase || 'Executando', 'active'));
  } else if (job && job.state === 'completed') {
    text('status-title', job.operation === 'inspect' ? 'Inspeção concluída'
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
    const operation = { inspect: 'Inspeção', convert: 'Conversão', install: 'Instalação' }[item.operation] || item.operation;
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
  renderHistory();
  refreshActions();
  text('host-label', app.host
    ? 'Sistema: ' + app.host + (app.nativeTarget ? ' · formato nativo: ' + app.nativeTarget.toUpperCase() : '')
    : 'Sessão local');
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

const zone = $('drop-zone');
const input = $('file-input');
zone.addEventListener('click', () => input.click());
zone.addEventListener('keydown', (event) => {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault();
    input.click();
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
