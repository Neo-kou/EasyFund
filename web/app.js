// QDII 限购雷达 · V1 极简版
// fetch 一次 → 按指数分组渲染表格 → 状态/溢价着色。无框架无构建。

const STATUS_LABEL = {
  open: '开放申购',
  limited: '限大额',
  suspended: '暂停申购',
  closed: '封闭期',
  listed: '场内交易',
  raising: '认购期',
  unknown: '未知',
};

let ALL_FUNDS = [];
let showAll = false;

function statusCell(f) {
  const label = STATUS_LABEL[f.purchase_status] || f.status_text || '未知';
  return `<span class="st st-${f.purchase_status}">${label}</span>`;
}

// 状态优先于限额: 暂停/封闭的一律不显示残留限额数字;
// NULL 限额按状态区分: open=真无限额(源站占位 1e11), limited=未公布(源站字段为 0, 额度以公告为准)
function limitCell(f) {
  if (f.purchase_status !== 'limited' && f.purchase_status !== 'open') return '<span class="na">—</span>';
  if (f.daily_limit == null) {
    return f.purchase_status === 'limited'
      ? '<span class="na" title="基金公司已限购, 具体额度以公告为准">未公布</span>'
      : '<span class="na">无限额</span>';
  }
  return `${fmtMoney(f.daily_limit)}`;
}

function fmtMoney(v) {
  if (v >= 10000) return (v / 10000).toFixed(v % 10000 === 0 ? 0 : 1) + ' 万';
  return v.toFixed(0) + ' 元';
}

function feeCell(f) {
  if (f.purchase_fee == null) return '<span class="na">—</span>';
  return (f.purchase_fee * 100).toFixed(2) + '%';
}

function premiumCell(f) {
  if (!f.etf_code) return '<span class="na">—</span>';
  if (f.etf_premium == null) return '<span class="na">暂无</span>';
  const pct = f.etf_premium * 100;
  let cls = 'pm-flat';
  if (pct >= 5) cls = 'pm-high';
  else if (pct >= 2) cls = 'pm-mid';
  else if (pct < 0) cls = 'pm-neg';
  return `<span class="${cls}" title="ETF ${f.etf_code} 现价 / T-1净值 − 1">${pct.toFixed(1)}%</span>`;
}

function tagChips(f) {
  if (!showAll || !f.tags) return '';
  return f.tags.map(t => `<span class="tag">${t}</span>`).join('');
}

function render(data) {
  ALL_FUNDS = data.funds;
  const core = ALL_FUNDS.filter(f => f.is_core);
  document.getElementById('updated-at').textContent =
    `数据截至 ${data.updated_at} · 核心池 ${core.length} / 全部 ${ALL_FUNDS.length} 只`;
  const btn = document.getElementById('toggle-pool');
  btn.style.display = '';
  btn.onclick = () => { showAll = !showAll; draw(); };
  draw();
}

function draw() {
  const funds = showAll ? ALL_FUNDS : ALL_FUNDS.filter(f => f.is_core);
  document.getElementById('toggle-pool').textContent = showAll ? '只看核心池' : '查看全部基金';

  const groups = new Map();
  for (const f of funds) {
    if (!groups.has(f.index)) groups.set(f.index, []);
    groups.get(f.index).push(f);
  }

  const html = [...groups.entries()].map(([index, funds]) => `
    <section class="group">
      <h2>${index} <small>${funds.length} 只</small></h2>
      <div class="table-wrap">
      <table>
        <thead>
          <tr><th>代码</th><th>名称</th><th>状态</th><th>单日限额</th><th>折后费率</th><th>ETF 参考溢价</th></tr>
        </thead>
        <tbody>
          ${funds.map(f => `
          <tr>
            <td class="code">${f.code}</td>
            <td>${f.name}${tagChips(f)}</td>
            <td>${statusCell(f)}</td>
            <td>${limitCell(f)}</td>
            <td>${feeCell(f)}</td>
            <td>${premiumCell(f)}</td>
          </tr>`).join('')}
        </tbody>
      </table>
      </div>
    </section>`).join('');

  document.getElementById('groups').innerHTML = html;
}

fetch('/api/qdii')
  .then(r => { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
  .then(render)
  .catch(err => {
    document.getElementById('updated-at').textContent = '数据加载失败: ' + err.message;
  });
