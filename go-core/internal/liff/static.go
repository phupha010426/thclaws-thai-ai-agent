package liff

const indexHTML = `<!doctype html>
<html lang="th">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
  <meta name="theme-color" content="#0f766e">
  <title>ThaiAiAgent เลขาบัญชีครัวเรือน</title>
  <style>
    :root {
      color-scheme: light;
      --ink: #10201c;
      --muted: #60736d;
      --line: #dbe7e2;
      --paper: #f7f4ec;
      --panel: #ffffff;
      --green: #0f8a5f;
      --teal: #0f766e;
      --red: #c24136;
      --gold: #c08410;
      --blue: #2563eb;
      --slate: #475569;
      --shadow: 0 8px 24px rgba(20, 45, 38, .12);
    }
    * { box-sizing: border-box; }
    html { scroll-behavior: smooth; }
    body {
      margin: 0;
      font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: linear-gradient(180deg, #dff2ea 0, var(--paper) 300px);
      color: var(--ink);
      letter-spacing: 0;
    }
    .app { max-width: 720px; margin: 0 auto; padding: 12px 12px 96px; }
    .topbar {
      position: sticky;
      top: 0;
      z-index: 5;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 10px;
      padding: 8px 0;
      background: linear-gradient(180deg, rgba(223,242,234,.96), rgba(223,242,234,.82));
      backdrop-filter: blur(12px);
    }
    .brand { display: flex; align-items: center; gap: 8px; min-width: 0; }
    .mark {
      width: 36px; height: 36px; border-radius: 8px;
      display: grid; place-items: center;
      background: #0f766e; color: #fff; font-weight: 900;
      box-shadow: var(--shadow);
    }
    .brand strong { display: block; font-size: 14px; }
    .brand span { display: block; color: var(--muted); font-size: 11px; margin-top: 1px; }
    .sync { border: 1px solid var(--line); background: rgba(255,255,255,.82); color: var(--teal); border-radius: 8px; padding: 8px 10px; font-weight: 800; font-size: 12px; }
    .hero {
      margin-top: 8px;
      padding: 16px;
      border: 1px solid rgba(15,118,110,.16);
      border-radius: 8px;
      background: rgba(255,255,255,.9);
      box-shadow: var(--shadow);
    }
    .eyebrow { margin: 0 0 6px; color: var(--teal); font-size: 12px; font-weight: 900; }
    h1 { margin: 0; font-size: 25px; line-height: 1.18; }
    .sub { margin: 6px 0 0; color: var(--muted); font-size: 13px; line-height: 1.45; }
    .score-wrap { display: grid; grid-template-columns: 116px 1fr; gap: 14px; align-items: center; margin-top: 14px; }
    .score {
      --score: 60;
      width: 112px; height: 112px; border-radius: 50%;
      display: grid; place-items: center;
      background: conic-gradient(var(--teal) calc(var(--score) * 1%), #e5eee9 0);
      box-shadow: inset 0 0 0 1px rgba(0,0,0,.04);
    }
    .score-inner {
      width: 82px; height: 82px; border-radius: 50%;
      display: grid; place-items: center;
      background: #fff;
      border: 1px solid var(--line);
    }
    .score-num { font-size: 28px; font-weight: 950; color: var(--teal); line-height: 1; }
    .score-unit { color: var(--muted); font-size: 10px; margin-top: 1px; }
    .score-copy strong { display: block; font-size: 18px; }
    .score-copy span { display: block; color: var(--muted); font-size: 13px; line-height: 1.45; margin-top: 4px; }
    .quick {
      display: grid;
      grid-template-columns: repeat(4, 1fr);
      gap: 8px;
      margin-top: 12px;
    }
    .quick button {
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #fff;
      color: #173d35;
      min-height: 64px;
      padding: 8px 4px;
      display: grid;
      gap: 4px;
      justify-items: center;
      align-content: center;
      font-size: 12px;
      font-weight: 850;
      box-shadow: 0 4px 12px rgba(20,45,38,.06);
    }
    .quick .icon { width: 24px; height: 24px; border-radius: 8px; display: grid; place-items: center; color: #fff; background: var(--teal); font-weight: 900; }
    .quick .red .icon { background: var(--red); }
    .quick .blue .icon { background: var(--blue); }
    .quick .gold .icon { background: var(--gold); }
    .range-panel {
      margin-top: 12px;
      display: grid;
      gap: 9px;
      padding: 10px;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #f9fcfa;
    }
    .range-row { display: flex; gap: 7px; overflow-x: auto; scrollbar-width: none; }
    .range-row button {
      flex: 0 0 auto;
      border: 1px solid var(--line);
      border-radius: 8px;
      background: #fff;
      color: #173d35;
      padding: 8px 10px;
      font-size: 12px;
      font-weight: 850;
    }
    .range-row button.active { background: var(--teal); border-color: var(--teal); color: #fff; }
    .custom-range { display: grid; grid-template-columns: 1fr 1fr auto; gap: 7px; }
    .custom-range input {
      min-width: 0;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 9px 8px;
      font: inherit;
      font-size: 12px;
      background: #fff;
    }
    .custom-range button {
      border: 0;
      border-radius: 8px;
      padding: 0 12px;
      color: #fff;
      background: var(--slate);
      font-weight: 900;
      font-size: 12px;
    }
    .grid { display: grid; gap: 10px; }
    .cards { grid-template-columns: repeat(3, 1fr); margin-top: 12px; }
    .card {
      background: rgba(255,255,255,.94);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
      padding: 12px;
    }
    .stat .label { color: var(--muted); font-size: 12px; font-weight: 750; }
    .stat .value { margin-top: 5px; font-size: 19px; font-weight: 950; white-space: nowrap; }
    .income { color: var(--green); }
    .expense { color: var(--red); }
    .balance { color: var(--blue); }
    .navchips {
      display: flex; gap: 8px; overflow-x: auto; padding: 12px 1px 2px; scrollbar-width: none;
    }
    .navchips a {
      flex: 0 0 auto;
      color: #173d35;
      text-decoration: none;
      border: 1px solid var(--line);
      background: rgba(255,255,255,.86);
      border-radius: 999px;
      padding: 8px 12px;
      font-size: 13px;
      font-weight: 850;
    }
    .section { margin-top: 14px; scroll-margin-top: 70px; }
    .section-title { display: flex; justify-content: space-between; align-items: baseline; gap: 10px; margin: 0 0 8px; }
    .section-title h2 { margin: 0; font-size: 17px; }
    .section-title span { color: var(--muted); font-size: 12px; }
    .insight-list { display: grid; gap: 8px; }
    .note {
      display: grid;
      grid-template-columns: 26px 1fr;
      gap: 9px;
      align-items: start;
      color: #31443e;
      font-size: 14px;
      line-height: 1.45;
    }
    .pill {
      height: 26px; min-width: 26px; display: inline-grid; place-items: center;
      border-radius: 999px; background: #e4f4ec; color: var(--green); font-weight: 950; font-size: 12px;
    }
    .analytics-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .metric .label { color: var(--muted); font-size: 12px; font-weight: 750; }
    .metric .value { margin-top: 6px; font-size: 20px; font-weight: 950; line-height: 1.2; }
    .metric .caption { margin-top: 4px; color: var(--muted); font-size: 12px; line-height: 1.35; }
    .progress { height: 10px; background: #e7eee9; border-radius: 999px; overflow: hidden; margin-top: 10px; }
    .progress span { display: block; height: 100%; background: var(--teal); width: 0; border-radius: inherit; }
    .chart-card { padding: 12px; }
    canvas { width: 100%; height: 230px; display: block; }
    .bars { display: grid; gap: 9px; }
    .bar-row { display: grid; grid-template-columns: 96px 1fr 58px; gap: 8px; align-items: center; font-size: 13px; }
    .bar-label { color: #243a34; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .bar-track { height: 10px; background: #e7eee9; border-radius: 999px; overflow: hidden; }
    .bar-fill { height: 100%; background: var(--gold); border-radius: inherit; }
    .bar-value { text-align: right; color: var(--muted); font-variant-numeric: tabular-nums; }
    .ledger-check { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
    .check-box { background: #f7faf8; border: 1px solid var(--line); border-radius: 8px; padding: 10px; }
    .check-box span { color: var(--muted); font-size: 12px; }
    .check-box strong { display: block; margin-top: 4px; font-size: 18px; }
    .timeline { display: grid; gap: 8px; }
    .tx {
      display: grid;
      grid-template-columns: 8px 1fr auto;
      gap: 10px;
      align-items: center;
      padding: 10px 0;
      border-bottom: 1px solid #edf2ef;
    }
    .tx:last-child { border-bottom: 0; }
    .dot { width: 8px; height: 38px; border-radius: 999px; background: var(--red); }
    .dot.in { background: var(--green); }
    .tx-title { font-size: 14px; font-weight: 900; }
    .tx-meta { margin-top: 2px; color: var(--muted); font-size: 12px; line-height: 1.35; }
    .tx-amount { font-weight: 950; font-size: 14px; white-space: nowrap; }
    .docs-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
    .doc-card {
      border: 1px solid var(--line);
      border-radius: 8px;
      overflow: hidden;
      background: #fff;
    }
    .doc-thumb {
      width: 100%;
      aspect-ratio: 4 / 3;
      object-fit: cover;
      background: #eef4f1;
      display: block;
    }
    .doc-body { padding: 9px; }
    .doc-title { font-size: 13px; font-weight: 950; line-height: 1.25; }
    .doc-meta { margin-top: 4px; color: var(--muted); font-size: 11px; line-height: 1.35; }
    .doc-badge { display: inline-block; margin-top: 6px; border-radius: 999px; padding: 4px 7px; background: #e8f2ff; color: var(--blue); font-size: 11px; font-weight: 850; }
    .doc-badge.out { background: #fff1f2; color: var(--red); }
    .doc-badge.in { background: #e7f7ef; color: var(--green); }
    .project-form { display: grid; grid-template-columns: 1.2fr .8fr .9fr 56px auto; gap: 8px; margin-bottom: 10px; }
    .project-form input, .edit-grid input, .edit-grid select {
      min-width: 0;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px;
      font: inherit;
      font-size: 13px;
      background: #fff;
    }
    .project-form button, .modal-actions button, .tx-edit {
      border: 0;
      border-radius: 8px;
      padding: 9px 10px;
      font-weight: 900;
      font-size: 12px;
      color: #fff;
      background: var(--teal);
    }
    .project-list { display: grid; gap: 8px; }
    .project-card { border: 1px solid var(--line); border-radius: 8px; padding: 10px; background: #fff; }
    .project-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
    .project-name { display: flex; align-items: center; gap: 8px; font-weight: 950; }
    .swatch { width: 12px; height: 12px; border-radius: 50%; background: var(--teal); }
    .project-total { margin-top: 7px; display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; color: var(--muted); font-size: 11px; }
    .project-total strong { display: block; margin-top: 2px; color: var(--ink); font-size: 13px; }
    .project-meta { margin-top: 8px; color: var(--muted); font-size: 12px; line-height: 1.4; }
    .review-badge { display: inline-block; border-radius: 999px; padding: 3px 7px; background: #fff7ed; color: #c2410c; font-size: 11px; font-weight: 900; }
    .project-actions { display: flex; gap: 6px; }
    .project-actions button, .tx-edit { background: var(--slate); color: #fff; }
    .tx-side { display: grid; gap: 5px; justify-items: end; }
    .tx-projects { margin-top: 4px; color: var(--blue); font-size: 11px; line-height: 1.35; }
    .modal {
      position: fixed; inset: 0; z-index: 50;
      display: none; align-items: end;
      background: rgba(15, 23, 42, .42);
    }
    .modal.show { display: flex; }
    .modal-panel {
      width: 100%; max-width: 720px; margin: 0 auto;
      max-height: 90vh; overflow: auto;
      border-radius: 14px 14px 0 0;
      background: var(--paper);
      padding: 14px;
      box-shadow: 0 -16px 40px rgba(0,0,0,.18);
    }
    .modal-title { display: flex; justify-content: space-between; align-items: center; gap: 8px; margin-bottom: 10px; }
    .modal-title h2 { margin: 0; font-size: 18px; }
    .modal-title button { border: 0; background: transparent; font-size: 24px; line-height: 1; }
    .edit-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
    .edit-grid label, .alloc-row label { display: grid; gap: 4px; color: var(--muted); font-size: 11px; font-weight: 800; }
    .edit-grid .wide { grid-column: 1 / -1; }
    .alloc-list { display: grid; gap: 7px; margin-top: 8px; }
    .alloc-row { display: grid; grid-template-columns: 1fr 130px; gap: 8px; align-items: end; }
    .modal-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; margin-top: 12px; }
    .modal-actions .danger { background: var(--red); }
    .bottom {
      position: fixed;
      left: 0; right: 0; bottom: 0;
      padding: 9px 10px calc(9px + env(safe-area-inset-bottom));
      background: rgba(247, 244, 236, .94);
      backdrop-filter: blur(12px);
      border-top: 1px solid var(--line);
      z-index: 10;
    }
    .bottom-inner { max-width: 720px; margin: 0 auto; display: grid; grid-template-columns: repeat(4, 1fr); gap: 7px; }
    .bottom button {
      border: 0;
      border-radius: 8px;
      padding: 9px 4px;
      min-height: 50px;
      font-size: 12px;
      font-weight: 900;
      color: #fff;
      background: var(--teal);
      text-align: center;
    }
    .bottom .red { background: var(--red); }
    .bottom .blue { background: var(--blue); }
    .bottom .slate { background: var(--slate); }
    .toast {
      position: fixed;
      left: 16px; right: 16px; bottom: 88px;
      max-width: 680px; margin: 0 auto;
      background: #10201c; color: #fff;
      border-radius: 8px;
      padding: 12px;
      font-size: 13px;
      line-height: 1.4;
      box-shadow: var(--shadow);
      display: none;
      z-index: 20;
    }
    .empty { color: var(--muted); font-size: 14px; line-height: 1.5; padding: 8px 0; }
    .error { background: #fff1f2; color: #9f1239; border: 1px solid #fecdd3; border-radius: 8px; padding: 12px; font-size: 14px; }
    .skeleton { min-height: 80px; background: linear-gradient(90deg, #eef4f1, #fff, #eef4f1); background-size: 200% 100%; animation: pulse 1.1s infinite; }
    @keyframes pulse { to { background-position: -200% 0; } }
    @media (max-width: 430px) {
      .app { padding-left: 10px; padding-right: 10px; }
      h1 { font-size: 22px; }
      .score-wrap { grid-template-columns: 104px 1fr; gap: 12px; }
      .score { width: 100px; height: 100px; }
      .score-inner { width: 74px; height: 74px; }
      .quick { grid-template-columns: repeat(4, minmax(0, 1fr)); }
      .quick button { min-height: 60px; font-size: 11px; }
      .custom-range { grid-template-columns: 1fr 1fr; }
      .custom-range button { grid-column: 1 / -1; min-height: 38px; }
      .project-form { grid-template-columns: 1fr 1fr; }
      .project-form button { grid-column: 1 / -1; }
      .edit-grid, .alloc-row { grid-template-columns: 1fr; }
      .cards { grid-template-columns: 1fr; }
      .analytics-grid { grid-template-columns: 1fr; }
      .docs-grid { grid-template-columns: 1fr; }
      .bar-row { grid-template-columns: 86px 1fr 54px; }
      canvas { height: 205px; }
    }
  </style>
  <script src="https://static.line-scdn.net/liff/edge/2/sdk.js"></script>
</head>
<body>
  <main class="app">
    <div class="topbar">
      <div class="brand">
        <div class="mark">฿</div>
        <div><strong>ThaiAiAgent</strong><span>เลขาบัญชีครัวเรือน</span></div>
      </div>
      <button class="sync" onclick="loadDashboard()">รีเฟรช</button>
    </div>

    <section class="hero" id="home">
      <p class="eyebrow">สมุดบัญชีของฉัน</p>
      <h1>ดูเงินให้รู้เรื่อง แบบเลขาคู่ใจ</h1>
      <p class="sub" id="updated">กำลังโหลดข้อมูลจริงของคุณเท่านั้น</p>
      <div class="score-wrap">
        <div class="score" id="scoreRing"><div class="score-inner"><div><div class="score-num" id="scoreNum">-</div><div class="score-unit">คะแนน</div></div></div></div>
        <div class="score-copy"><strong id="scoreLabel">กำลังตรวจบัญชี</strong><span id="scoreText">วิเคราะห์จากรายการจริงที่คุณบันทึกไว้ ไม่เดาข้อมูลเพิ่ม</span></div>
      </div>
      <div class="quick" aria-label="เมนูเร็ว">
        <button class="red" onclick="sendLineCommand('บันทึกรายจ่าย')"><span class="icon">-</span><span>รายจ่าย</span></button>
        <button onclick="sendLineCommand('บันทึกรายรับ')"><span class="icon">+</span><span>รายรับ</span></button>
        <button class="blue" onclick="sendLineCommand('อ่านสลิป')"><span class="icon">S</span><span>อ่านสลิป</span></button>
        <button class="gold" onclick="sendLineCommand('ช่วยวิเคราะห์การเงิน')"><span class="icon">A</span><span>วิเคราะห์</span></button>
      </div>
      <div class="range-panel" aria-label="เลือกช่วงวันที่">
        <div class="range-row" id="presetButtons">
          <button data-preset="today" onclick="setPreset('today')">วันนี้</button>
          <button data-preset="last_7_days" onclick="setPreset('last_7_days')">7 วัน</button>
          <button data-preset="last_30_days" onclick="setPreset('last_30_days')">30 วัน</button>
          <button data-preset="this_month" onclick="setPreset('this_month')">เดือนนี้</button>
          <button data-preset="last_month" onclick="setPreset('last_month')">เดือนก่อน</button>
          <button data-preset="this_quarter" onclick="setPreset('this_quarter')">ไตรมาสนี้</button>
          <button data-preset="this_year" onclick="setPreset('this_year')">ปีนี้</button>
        </div>
        <div class="custom-range">
          <input type="date" id="fromDate" aria-label="วันที่เริ่มต้น">
          <input type="date" id="toDate" aria-label="วันที่สิ้นสุด">
          <button onclick="applyCustomRange()">ดูช่วงนี้</button>
        </div>
      </div>
    </section>

    <section class="grid cards" aria-label="ภาพรวม">
      <article class="card stat"><div class="label">เงินเข้าช่วงนี้</div><div class="value income" id="monthIncome">-</div></article>
      <article class="card stat"><div class="label">เงินออกช่วงนี้</div><div class="value expense" id="monthExpense">-</div></article>
      <article class="card stat"><div class="label">สุทธิช่วงนี้</div><div class="value balance" id="monthBalance">-</div></article>
    </section>

    <nav class="navchips" aria-label="ไปยังส่วนต่างๆ">
      <a href="#insights">วิเคราะห์หนักๆ</a>
      <a href="#cashbook">เงินเข้าออก</a>
      <a href="#documents">เอกสาร/รูปภาพ</a>
      <a href="#projects">โครงการ</a>
      <a href="#review">รอตรวจ</a>
      <a href="#charts">กราฟ</a>
      <a href="#categories">หมวดรายจ่าย</a>
      <a href="#ledger">บัญชีแยกประเภท</a>
      <a href="#recent">รายการล่าสุด</a>
    </nav>

    <section class="section" id="insights">
      <div class="section-title"><h2>วิเคราะห์หนักๆ</h2><span id="todayLabel">-</span></div>
      <div class="card insight-list" id="notes"><div class="skeleton"></div></div>
    </section>

    <section class="section">
      <div class="section-title"><h2>ตัวเลขที่ควรรู้</h2><span id="monthLabel">เดือนนี้</span></div>
      <div class="grid analytics-grid">
        <article class="card metric"><div class="label">เฉลี่ยใช้ต่อวัน</div><div class="value expense" id="dailyAvg">-</div><div class="caption">คำนวณจากวันที่ผ่านไปในเดือนนี้</div></article>
        <article class="card metric"><div class="label">คาดการณ์สิ้นเดือน</div><div class="value" id="projected">-</div><div class="caption" id="paceText">-</div></article>
        <article class="card metric"><div class="label">แนวโน้ม 7 วัน</div><div class="value" id="weekTrend">-</div><div class="caption" id="weekTrendText">-</div></article>
        <article class="card metric"><div class="label">ความคืบหน้าเดือน</div><div class="value" id="monthProgress">-</div><div class="progress"><span id="monthProgressBar"></span></div></article>
      </div>
    </section>

    <section class="section" id="cashbook">
      <div class="section-title"><h2>เงินเข้าออก</h2><span id="cashbookLabel">ตามช่วงวันที่</span></div>
      <article class="card"><div class="timeline" id="cashbookList"></div></article>
    </section>

    <section class="section" id="documents">
      <div class="section-title"><h2>เอกสารและรูปภาพ</h2><span id="documentCount">รูปทั้งหมด</span></div>
      <article class="card"><div class="docs-grid" id="documentList"></div></article>
    </section>

    <section class="section" id="projects">
      <div class="section-title"><h2>โครงการ</h2><span>แยกรายรับรายจ่ายตามงาน</span></div>
      <article class="card">
        <div class="project-form">
          <input id="projectName" maxlength="80" placeholder="ชื่อโครงการ">
          <input id="projectBudget" type="number" min="0" step="0.01" placeholder="งบประมาณ">
          <input id="projectTargetDate" type="date" aria-label="วันเป้าหมาย">
          <input id="projectColor" type="color" value="#0f766e" aria-label="สีโครงการ">
          <button onclick="createProject()">เพิ่ม</button>
        </div>
        <div class="project-list" id="projectList"></div>
      </article>
    </section>

    <section class="section" id="review">
      <div class="section-title"><h2>รอตรวจจัดสรร</h2><span id="reviewCount">-</span></div>
      <article class="card">
        <p class="sub" style="margin-top:0">รายการที่ยังไม่ได้ผูกโครงการ จะขึ้นตรงนี้เพื่อให้กดแก้และ allocate ได้เร็ว</p>
        <div class="timeline" id="reviewList"></div>
      </article>
    </section>

    <section class="section" id="charts">
      <div class="section-title"><h2>กราฟรับ-จ่าย</h2><span id="chartRange">ตามเวลาไทย</span></div>
      <article class="card chart-card"><canvas id="cashflow" width="640" height="300"></canvas></article>
    </section>

    <section class="section" id="categories">
      <div class="section-title"><h2>หมวดรายจ่ายยอดนิยม</h2><span>ดูว่าหมวดไหนกินเงิน</span></div>
      <article class="card"><div class="bars" id="categoryBars"></div></article>
    </section>

    <section class="section" id="ledger">
      <div class="section-title"><h2>บัญชีแยกประเภท</h2><span>เดบิต = เครดิต</span></div>
      <article class="card">
        <div class="ledger-check">
          <div class="check-box"><span>เดบิต</span><strong id="debit">-</strong></div>
          <div class="check-box"><span>เครดิต</span><strong id="credit">-</strong></div>
        </div>
        <p class="sub" id="ledgerText"></p>
      </article>
    </section>

    <section class="section" id="recent">
      <div class="section-title"><h2>รายการล่าสุด</h2><span>จาก ledger จริง</span></div>
      <article class="card"><div class="timeline" id="recentList"></div></article>
    </section>
  </main>

  <nav class="bottom" aria-label="เมนูหลัก">
    <div class="bottom-inner">
      <button onclick="scrollToId('home')">สมุดบัญชี</button>
      <button class="blue" onclick="scrollToId('documents')">เอกสาร</button>
      <button class="slate" onclick="scrollToId('projects')">โครงการ</button>
      <button class="red" onclick="sendLineCommand('บันทึกรายจ่าย')">บันทึก</button>
    </div>
  </nav>
  <div class="modal" id="txModal" role="dialog" aria-modal="true" aria-labelledby="txModalTitle">
    <div class="modal-panel">
      <div class="modal-title">
        <h2 id="txModalTitle">แก้รายการบัญชี</h2>
        <button onclick="closeTransactionEditor()" aria-label="ปิด">×</button>
      </div>
      <div class="edit-grid">
        <label>ประเภท<select id="editType"><option value="EXPENSE">รายจ่าย</option><option value="INCOME">รายรับ</option></select></label>
        <label>จำนวนเงิน<input id="editAmount" type="number" step="0.01" min="0"></label>
        <label>หมวด<input id="editCategory" maxlength="80"></label>
        <label>วันเวลา<input id="editHappenedAt" type="datetime-local"></label>
        <label class="wide">รายละเอียด<input id="editNote" maxlength="200"></label>
        <label>รับจาก/จ่ายให้<input id="editCounterpartyName" maxlength="120"></label>
        <label>บทบาท<select id="editCounterpartyRole"><option value="">ไม่ระบุ</option><option value="from">รับจาก</option><option value="to">จ่าย/โอนให้</option></select></label>
      </div>
      <div class="section-title" style="margin-top:12px"><h2>จัดสรรเข้าโครงการ</h2><span id="allocSum">0 บาท</span></div>
      <div class="alloc-list" id="allocationList"></div>
      <div class="modal-actions">
        <button class="danger" onclick="deleteEditingTransaction()">ลบรายการ</button>
        <button onclick="saveEditingTransaction()">บันทึก</button>
      </div>
    </div>
  </div>
  <div class="toast" id="toast"></div>

  <script>
    const signedToken = readToken();
    const configuredLiffId = __LIFF_ID_JSON__;
    const state = {
      data: null,
      liffReady: false,
      preset: 'this_month',
      from: '',
      to: '',
      loading: false,
      failures: 0,
      autoRefreshTimer: 0,
      retryTimer: 0,
      editingTx: null
    };

    function readToken() {
      const params = new URLSearchParams(location.search);
      const direct = params.get('t');
      if (direct) return direct;
      const liffState = params.get('liff.state');
      if (!liffState) return '';
      const qIndex = liffState.indexOf('?');
      const stateText = qIndex >= 0 ? liffState.slice(qIndex) : (liffState.startsWith('?') ? liffState : '?' + liffState);
      return new URLSearchParams(stateText).get('t') || '';
    }

    function numberText(v, maximumFractionDigits = 0) {
      return new Intl.NumberFormat('th-TH', { maximumFractionDigits }).format(Number(v || 0));
    }

    function money(v) {
      return numberText(v, 2);
    }

    async function loadDashboard(options = {}) {
      if (state.loading) return;
      state.loading = true;
      clearTimeout(state.retryTimer);
      if (!options.silent) {
        document.getElementById('updated').textContent = 'กำลังโหลดข้อมูลล่าสุด...';
      }
      try {
        const headers = { 'accept': 'application/json' };
        let url = '/liff/api/dashboard' + rangeQuery();
        if (signedToken) {
          url += (url.includes('?') ? '&' : '?') + 't=' + encodeURIComponent(signedToken);
        } else {
          const idToken = await getLineIDToken();
          if (!idToken) return;
          headers.Authorization = 'Bearer ' + idToken;
        }
        const res = await fetch(url, { headers });
        const json = await res.json();
        if (!res.ok || !json.ok) throw new Error(json.error || 'โหลดข้อมูลไม่สำเร็จ');
        state.data = json.data;
        state.failures = 0;
        render(json.data);
        scheduleAutoRefresh();
      } catch (err) {
        state.failures += 1;
        const wait = scheduleRetry();
        showError('โหลดข้อมูลไม่สำเร็จครับ ระบบจะลองใหม่อัตโนมัติใน ' + wait + ' วินาที');
      } finally {
        state.loading = false;
      }
    }

    async function apiFetch(path, options = {}) {
      const headers = Object.assign({ 'accept': 'application/json' }, options.headers || {});
      let url = path;
      if (signedToken) {
        url += (url.includes('?') ? '&' : '?') + 't=' + encodeURIComponent(signedToken);
      } else {
        const idToken = await getLineIDToken();
        if (!idToken) throw new Error('auth_failed');
        headers.Authorization = 'Bearer ' + idToken;
      }
      if (options.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
      const res = await fetch(url, Object.assign({}, options, { headers }));
      const json = await res.json().catch(() => ({}));
      if (!res.ok || !json.ok) throw new Error(json.error || 'request_failed');
      return json;
    }

    function scheduleAutoRefresh() {
      clearTimeout(state.autoRefreshTimer);
      state.autoRefreshTimer = setTimeout(() => {
        if (!document.hidden) loadDashboard({ silent: true });
        else scheduleAutoRefresh();
      }, 30000);
    }

    function scheduleRetry() {
      const wait = Math.min(30, Math.max(3, state.failures * 5));
      clearTimeout(state.retryTimer);
      state.retryTimer = setTimeout(() => loadDashboard({ silent: true }), wait * 1000);
      return wait;
    }

    function rangeQuery() {
      const p = new URLSearchParams();
      if (state.from && state.to) {
        p.set('from', state.from);
        p.set('to', state.to);
      } else {
        p.set('preset', state.preset || 'this_month');
      }
      return '?' + p.toString();
    }

    function setPreset(preset) {
      state.preset = preset;
      state.from = '';
      state.to = '';
      document.getElementById('fromDate').value = '';
      document.getElementById('toDate').value = '';
      updatePresetButtons();
      loadDashboard();
    }

    function applyCustomRange() {
      const from = document.getElementById('fromDate').value;
      const to = document.getElementById('toDate').value;
      if (!from || !to) {
        showToast('เลือกวันที่เริ่มต้นและสิ้นสุดก่อนครับ');
        return;
      }
      if (to < from) {
        showToast('วันที่สิ้นสุดต้องไม่ก่อนวันที่เริ่มต้นครับ');
        return;
      }
      state.preset = 'custom';
      state.from = from;
      state.to = to;
      updatePresetButtons();
      loadDashboard();
    }

    function updatePresetButtons() {
      document.querySelectorAll('#presetButtons button').forEach(btn => {
        btn.classList.toggle('active', btn.dataset.preset === state.preset && !state.from);
      });
    }

    async function getLineIDToken() {
      if (!configuredLiffId) {
        showError('ระบบยังไม่ได้ตั้งค่า LIFF_ID ครับ ตอนนี้ให้พิมพ์ เปิดสมุดบัญชี ในแชทเพื่อรับปุ่มเปิดชั่วคราวก่อน');
        return '';
      }
      if (!window.liff) {
        showError('โหลด LIFF SDK ไม่สำเร็จครับ ลองเปิดใหม่อีกครั้งในแอป LINE');
        return '';
      }
      if (!state.liffReady) {
        try {
          await liff.init({ liffId: configuredLiffId });
        } catch (err) {
          if (redirectToLiffApp()) return '';
          throw err;
        }
        state.liffReady = true;
      }
      if (!liff.isLoggedIn()) {
        liff.login({ redirectUri: location.href });
        return '';
      }
      const idToken = liff.getIDToken();
      if (!idToken) {
        if (redirectToLiffApp()) return '';
        showError('ยืนยันตัวตน LINE ไม่สำเร็จครับ ระบบจะลองใหม่ให้อัตโนมัติ');
        return '';
      }
      return idToken;
    }

    function redirectToLiffApp() {
      if (!configuredLiffId) return false;
      if (sessionStorage.getItem('thaiAiAgentLiffRedirectTried') === '1') return false;
      sessionStorage.setItem('thaiAiAgentLiffRedirectTried', '1');
      showError('กำลังเปิดผ่าน LINE Mini App อีกครั้ง เพื่อยืนยันตัวตนให้ถูกต้อง');
      setTimeout(() => {
        location.href = 'https://liff.line.me/' + encodeURIComponent(configuredLiffId);
      }, 500);
      return true;
    }

    function render(data) {
      const a = data.analytics || {};
      const range = data.range || {};
      state.preset = range.preset || state.preset;
      if (range.from) document.getElementById('fromDate').value = range.from;
      if (range.to) document.getElementById('toDate').value = range.to;
      updatePresetButtons();
      document.getElementById('updated').textContent = 'อัปเดต ' + data.updatedAt + ' • ' + (range.label || 'เดือนนี้') + ' • ข้อมูลแยกตาม LINE ID';
      document.getElementById('todayLabel').textContent = data.todayLabel;
      document.getElementById('monthLabel').textContent = data.monthLabel;
      document.getElementById('monthIncome').textContent = data.month.incomeText + ' บาท';
      document.getElementById('monthExpense').textContent = data.month.expenseText + ' บาท';
      document.getElementById('monthBalance').textContent = data.month.balanceText + ' บาท';
      document.getElementById('scoreRing').style.setProperty('--score', Number(a.score || 0));
      document.getElementById('scoreNum').textContent = numberText(a.score || 0);
      document.getElementById('scoreLabel').textContent = 'สุขภาพการเงิน: ' + (a.scoreLabel || '-');
      document.getElementById('scoreText').textContent = a.expensePaceText || 'วิเคราะห์จากรายการจริงที่บันทึกไว้เท่านั้น';
      document.getElementById('dailyAvg').textContent = (a.dailyAverageExpenseText || '0') + ' บาท';
      document.getElementById('projected').textContent = (a.projectedMonthExpenseText || '0') + ' บาท';
      document.getElementById('paceText').textContent = a.expensePaceText || '-';
      document.getElementById('weekTrend').textContent = trendValue(a.weekTrendPct);
      document.getElementById('weekTrendText').textContent = a.weekTrendText || '-';
      document.getElementById('monthProgress').textContent = numberText(a.elapsedDays || 0) + '/' + numberText(a.daysInMonth || 0) + ' วัน';
      document.getElementById('monthProgressBar').style.width = Math.max(0, Math.min(100, a.monthProgressPct || 0)) + '%';
      document.getElementById('cashbookLabel').textContent = range.label || 'ตามช่วงวันที่';
      document.getElementById('chartRange').textContent = range.label || 'ตามเวลาไทย';
      renderNotes(data.accountantNotes || []);
      renderBars(data.topCategories || []);
      renderLedger(data.ledgerCheck || {});
      renderCashbook(data.recent || []);
      renderRecent(data.recent || []);
      renderDocuments(data.documents || {});
      renderProjects(data.projects || []);
      renderReview(data.recent || []);
      drawCashflow(data.cashflow || []);
    }

    function trendValue(v) {
      v = Number(v || 0);
      if (v === 0) return 'ทรงตัว';
      return (v > 0 ? '+' : '') + numberText(Math.round(v)) + '%';
    }

    function renderNotes(notes) {
      const root = document.getElementById('notes');
      root.innerHTML = '';
      if (!notes.length) notes = ['ยังไม่มีข้อมูลพอให้วิเคราะห์ครับ'];
      notes.slice(0, 8).forEach((text, i) => {
        const row = document.createElement('div');
        row.className = 'note';
        row.innerHTML = '<span class="pill">' + numberText(i + 1) + '</span><span></span>';
        row.lastElementChild.textContent = text;
        root.appendChild(row);
      });
    }

    function renderBars(items) {
      const root = document.getElementById('categoryBars');
      root.innerHTML = '';
      if (!items.length) {
        root.innerHTML = '<div class="empty">ยังไม่มีรายจ่ายเดือนนี้ครับ ส่งในแชทว่า กาแฟ 60 ได้เลย</div>';
        return;
      }
      items.forEach(item => {
        const row = document.createElement('div');
        row.className = 'bar-row';
        row.innerHTML = '<div class="bar-label"></div><div class="bar-track"><div class="bar-fill"></div></div><div class="bar-value"></div>';
        row.children[0].textContent = item.category;
        row.querySelector('.bar-fill').style.width = Math.max(4, Math.min(100, item.sharePct || 0)) + '%';
        row.children[2].textContent = item.text;
        root.appendChild(row);
      });
    }

    function renderLedger(check) {
      document.getElementById('debit').textContent = money(check.debit) + ' บาท';
      document.getElementById('credit').textContent = money(check.credit) + ' บาท';
      document.getElementById('ledgerText').textContent = check.text || 'ยังไม่มีรายการบัญชีแยกประเภทในเดือนนี้';
    }

    function renderCashbook(items) {
      const root = document.getElementById('cashbookList');
      root.innerHTML = '';
      if (!items.length) {
        root.innerHTML = '<div class="empty">ช่วงนี้ยังไม่มีเงินเข้าออกครับ</div>';
        return;
      }
      items.forEach(item => root.appendChild(txElement(item)));
    }

    function renderRecent(items) {
      const root = document.getElementById('recentList');
      root.innerHTML = '';
      if (!items.length) {
        root.innerHTML = '<div class="empty">ยังไม่มีรายการล่าสุดครับ</div>';
        return;
      }
      items.forEach(item => {
        root.appendChild(txElement(item));
      });
    }

    function renderReview(items) {
      const root = document.getElementById('reviewList');
      const count = document.getElementById('reviewCount');
      const pending = (items || []).filter(item => item.type && !(item.allocations || []).length);
      count.textContent = numberText(pending.length) + ' รายการ';
      root.innerHTML = '';
      if (!pending.length) {
        root.innerHTML = '<div class="empty">ไม่มีรายการค้างจัดสรรในช่วงนี้ครับ</div>';
        return;
      }
      pending.slice(0, 20).forEach(item => {
        const row = txElement(item);
        const badge = document.createElement('span');
        badge.className = 'review-badge';
        badge.textContent = 'ยังไม่เข้าโครงการ';
        row.children[1].appendChild(badge);
        root.appendChild(row);
      });
    }

    function txElement(item) {
      const inType = item.type === 'INCOME';
      const row = document.createElement('div');
      row.className = 'tx';
      row.innerHTML = '<span class="dot"></span><div><div class="tx-title"></div><div class="tx-meta"></div><div class="tx-projects"></div></div><div class="tx-side"><div class="tx-amount"></div><button class="tx-edit">แก้</button></div>';
      row.querySelector('.dot').className = 'dot' + (inType ? ' in' : '');
      row.querySelector('.tx-title').textContent = (item.note || item.category || 'รายการ') + ' • ' + item.typeLabel;
      const cp = item.counterpartyName ? ' • ' + (item.counterpartyRole === 'from' ? 'รับจาก ' : 'จ่าย/โอนให้ ') + item.counterpartyName : '';
      row.querySelector('.tx-meta').textContent = item.happenedAt + ' • ' + item.category + cp;
      const allocText = (item.allocations || []).map(a => a.projectName + ' ' + a.amountText).join(' • ');
      row.querySelector('.tx-projects').textContent = allocText ? 'โครงการ: ' + allocText : '';
      row.querySelector('.tx-amount').className = 'tx-amount ' + (inType ? 'income' : 'expense');
      row.querySelector('.tx-amount').textContent = (inType ? '+' : '-') + item.amountText;
      row.querySelector('.tx-edit').onclick = () => openTransactionEditor(item.id);
      return row;
    }

    function openTransactionEditor(id) {
      const tx = (state.data?.recent || []).find(x => x.id === id);
      if (!tx) {
        showToast('ไม่พบรายการนี้ในช่วงวันที่ที่เลือกครับ');
        return;
      }
      state.editingTx = tx;
      document.getElementById('editType').value = tx.type || 'EXPENSE';
      document.getElementById('editAmount').value = Number(tx.amount || 0);
      document.getElementById('editCategory').value = tx.category || '';
      document.getElementById('editNote').value = tx.note || '';
      document.getElementById('editCounterpartyName').value = tx.counterpartyName || '';
      document.getElementById('editCounterpartyRole').value = tx.counterpartyRole || '';
      document.getElementById('editHappenedAt').value = tx.happenedAtInput || '';
      renderAllocationEditor(tx);
      document.getElementById('txModal').classList.add('show');
    }

    function closeTransactionEditor() {
      state.editingTx = null;
      document.getElementById('txModal').classList.remove('show');
    }

    function renderAllocationEditor(tx) {
      const root = document.getElementById('allocationList');
      const projects = state.data?.projects || [];
      root.innerHTML = '';
      if (!projects.length) {
        root.innerHTML = '<div class="empty">ยังไม่มีโครงการครับ เพิ่มโครงการก่อนแล้วค่อยจัดสรรรายการนี้</div>';
        updateAllocSum();
        return;
      }
      const current = {};
      (tx.allocations || []).forEach(a => { current[a.projectId] = a.amount; });
      projects.forEach(p => {
        const row = document.createElement('div');
        row.className = 'alloc-row';
        row.innerHTML = '<label><span></span><input type="number" min="0" step="0.01"></label><label>หมายเหตุ<input maxlength="120"></label>';
        row.dataset.projectId = p.id;
        row.querySelector('span').textContent = p.name;
        const amount = row.querySelector('input[type="number"]');
        amount.value = current[p.id] ? Number(current[p.id]) : '';
        amount.oninput = updateAllocSum;
        root.appendChild(row);
      });
      updateAllocSum();
    }

    function collectAllocations() {
      const rows = Array.from(document.querySelectorAll('#allocationList .alloc-row'));
      return rows.map(row => {
        const inputs = row.querySelectorAll('input');
        return {
          projectId: row.dataset.projectId,
          amount: Number(inputs[0].value || 0),
          note: inputs[1].value || ''
        };
      }).filter(a => a.projectId && a.amount > 0);
    }

    function updateAllocSum() {
      const sum = collectAllocations().reduce((n, a) => n + a.amount, 0);
      document.getElementById('allocSum').textContent = money(sum) + ' บาท';
    }

    async function saveEditingTransaction() {
      if (!state.editingTx) return;
      const amount = Number(document.getElementById('editAmount').value || 0);
      if (amount <= 0) {
        showToast('จำนวนเงินต้องมากกว่า 0 ครับ');
        return;
      }
      const allocations = collectAllocations();
      const allocated = allocations.reduce((n, a) => n + a.amount, 0);
      if (allocated - amount > 0.01) {
        showToast('ยอดจัดสรรเข้าโครงการเกินจำนวนเงินของรายการครับ');
        return;
      }
      const payload = {
        type: document.getElementById('editType').value,
        amount,
        category: document.getElementById('editCategory').value.trim(),
        note: document.getElementById('editNote').value.trim(),
        counterpartyName: document.getElementById('editCounterpartyName').value.trim(),
        counterpartyRole: document.getElementById('editCounterpartyRole').value,
        happenedAt: document.getElementById('editHappenedAt').value,
        allocations
      };
      if (!payload.category || !payload.happenedAt) {
        showToast('กรอกหมวดและวันเวลาให้ครบก่อนครับ');
        return;
      }
      try {
        await apiFetch('/liff/api/transactions/' + encodeURIComponent(state.editingTx.id), {
          method: 'PATCH',
          body: JSON.stringify(payload)
        });
        closeTransactionEditor();
        showToast('บันทึกการแก้ไขแล้วครับ');
        await loadDashboard();
      } catch (err) {
        showToast('แก้รายการไม่สำเร็จครับ ตรวจยอดจัดสรรหรือข้อมูลอีกครั้ง');
      }
    }

    async function deleteEditingTransaction() {
      if (!state.editingTx) return;
      if (!confirm('ลบรายการนี้ออกจากการคำนวณใช่ไหมครับ?')) return;
      try {
        await apiFetch('/liff/api/transactions/' + encodeURIComponent(state.editingTx.id), { method: 'DELETE' });
        closeTransactionEditor();
        showToast('ลบรายการแล้วครับ');
        await loadDashboard();
      } catch (err) {
        showToast('ลบรายการไม่สำเร็จครับ');
      }
    }

    function renderDocuments(docs) {
      const root = document.getElementById('documentList');
      const items = docs.items || [];
      document.getElementById('documentCount').textContent = numberText(docs.imageCount || 0) + ' รูป • ' + numberText(docs.slipCount || 0) + ' สลิป';
      root.innerHTML = '';
      if (!items.length) {
        root.innerHTML = '<div class="empty">ช่วงนี้ยังไม่มีรูปหรือเอกสารครับ ส่งรูปสลิปในแชทได้เลย ระบบจะเก็บเป็นไฟล์ใน MinIO ไม่เก็บ base64</div>';
        return;
      }
      items.forEach(item => {
        const card = document.createElement('div');
        card.className = 'doc-card';
        const badgeClass = item.direction === 'INCOME' ? ' in' : (item.direction === 'EXPENSE' ? ' out' : '');
        card.innerHTML = '<img class="doc-thumb" alt=""><div class="doc-body"><div class="doc-title"></div><div class="doc-meta"></div><span class="doc-badge"></span></div>';
        const img = card.querySelector('img');
        if (item.imageUrl) img.src = item.imageUrl;
        img.alt = item.kindLabel || 'เอกสาร';
        const title = item.kind === 'slip'
          ? ((item.directionLabel || 'สลิป') + (item.amountText ? ' • ' + item.amountText + ' บาท' : ''))
          : (item.kindLabel || 'รูปภาพ');
        card.querySelector('.doc-title').textContent = title;
        const accounts = [item.fromAccountMasked && 'จาก ' + item.fromAccountMasked, item.toAccountMasked && 'ไป ' + item.toAccountMasked].filter(Boolean).join(' • ');
        card.querySelector('.doc-meta').textContent = item.createdAt + (accounts ? ' • ' + accounts : '') + (item.sizeText ? ' • ' + item.sizeText : '');
        const badge = card.querySelector('.doc-badge');
        badge.className = 'doc-badge' + badgeClass;
        badge.textContent = item.kind === 'slip' ? (item.status || 'รอตรวจ') : 'รูปอื่นๆ';
        root.appendChild(card);
      });
    }

    function renderProjects(projects) {
      const root = document.getElementById('projectList');
      root.innerHTML = '';
      if (!projects.length) {
        root.innerHTML = '<div class="empty">ยังไม่มีโครงการครับ</div>';
        return;
      }
      projects.forEach(p => {
        const card = document.createElement('div');
        card.className = 'project-card';
        card.innerHTML =
          '<div class="project-head"><div class="project-name"><span class="swatch"></span><span></span></div><div class="project-actions"><button>แก้</button><button>ปิด</button></div></div>' +
          '<div class="project-total"><span>รับ<strong></strong></span><span>จ่าย<strong></strong></span><span>สุทธิ<strong></strong></span></div>' +
          '<div class="project-total"><span>งบ<strong></strong></span><span>ใช้ไป<strong></strong></span><span>เหลืองบ<strong></strong></span></div>' +
          '<div class="progress"><span></span></div><div class="project-meta"></div>';
        card.querySelector('.swatch').style.background = p.color || '#0f766e';
        card.querySelector('.project-name span:last-child').textContent = p.name;
        const totals = card.querySelectorAll('.project-total strong');
        totals[0].textContent = (p.incomeText || '0') + ' บาท';
        totals[1].textContent = (p.expenseText || '0') + ' บาท';
        totals[2].textContent = (p.balanceText || '0') + ' บาท';
        totals[3].textContent = p.budgetAmount ? (p.budgetText || money(p.budgetAmount)) + ' บาท' : '-';
        totals[4].textContent = (p.expenseText || '0') + ' บาท';
        totals[5].textContent = p.budgetAmount ? (p.remainingBudgetText || '0') + ' บาท' : '-';
        card.querySelector('.progress span').style.width = p.budgetAmount ? Math.max(2, Math.min(100, p.budgetUsedPct || 0)) + '%' : '0';
        card.querySelector('.project-meta').textContent = (p.targetDate ? 'เป้าหมาย ' + p.targetDate + ' • ' : '') + (p.budgetAmount ? 'ใช้งบไป ' + numberText(p.budgetUsedPct || 0, 1) + '%' : 'ยังไม่ได้ตั้งงบประมาณ');
        const buttons = card.querySelectorAll('button');
        buttons[0].onclick = () => renameProject(p);
        buttons[1].onclick = () => deleteProject(p);
        root.appendChild(card);
      });
    }

    async function createProject() {
      const nameEl = document.getElementById('projectName');
      const name = nameEl.value.trim();
      if (!name) {
        showToast('ใส่ชื่อโครงการก่อนครับ');
        return;
      }
      try {
        await apiFetch('/liff/api/projects', {
          method: 'POST',
          body: JSON.stringify({
            name,
            color: document.getElementById('projectColor').value || '#0f766e',
            budgetAmount: Number(document.getElementById('projectBudget').value || 0),
            targetDate: document.getElementById('projectTargetDate').value || ''
          })
        });
        nameEl.value = '';
        document.getElementById('projectBudget').value = '';
        document.getElementById('projectTargetDate').value = '';
        showToast('เพิ่มโครงการแล้วครับ');
        await loadDashboard();
      } catch (err) {
        showToast('เพิ่มโครงการไม่สำเร็จครับ ชื่ออาจซ้ำกัน');
      }
    }

    async function renameProject(project) {
      const name = prompt('ชื่อโครงการ', project.name || '');
      if (!name || !name.trim()) return;
      const budgetText = prompt('งบประมาณโครงการ (เว้นว่างถ้าไม่ตั้ง)', project.budgetAmount ? String(project.budgetAmount) : '');
      if (budgetText === null) return;
      const targetDate = prompt('วันเป้าหมาย YYYY-MM-DD (เว้นว่างถ้าไม่ตั้ง)', project.targetDateInput || '');
      if (targetDate === null) return;
      try {
        await apiFetch('/liff/api/projects/' + encodeURIComponent(project.id), {
          method: 'PATCH',
          body: JSON.stringify({
            name: name.trim(),
            color: project.color || '#0f766e',
            description: project.description || '',
            budgetAmount: Number(budgetText || 0),
            targetDate: targetDate.trim()
          })
        });
        showToast('อัปเดตโครงการแล้วครับ');
        await loadDashboard();
      } catch (err) {
        showToast('อัปเดตโครงการไม่สำเร็จครับ ตรวจวันที่หรืองบประมาณอีกครั้ง');
      }
    }

    async function deleteProject(project) {
      if (!confirm('ปิดโครงการ "' + project.name + '" ใช่ไหมครับ?')) return;
      try {
        await apiFetch('/liff/api/projects/' + encodeURIComponent(project.id), { method: 'DELETE' });
        showToast('ปิดโครงการแล้วครับ');
        await loadDashboard();
      } catch (err) {
        showToast('ปิดโครงการไม่สำเร็จครับ');
      }
    }

    function drawCashflow(points) {
      const canvas = document.getElementById('cashflow');
      const ctx = canvas.getContext('2d');
      const dpr = window.devicePixelRatio || 1;
      const cssW = canvas.clientWidth;
      const cssH = canvas.clientHeight;
      canvas.width = Math.floor(cssW * dpr);
      canvas.height = Math.floor(cssH * dpr);
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
      ctx.clearRect(0, 0, cssW, cssH);
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, cssW, cssH);
      const pad = { l: 34, r: 12, t: 18, b: 36 };
      const w = cssW - pad.l - pad.r;
      const h = cssH - pad.t - pad.b;
      const max = Math.max(100, ...points.flatMap(p => [p.income || 0, p.expense || 0]));
      ctx.strokeStyle = '#d8e2de';
      ctx.lineWidth = 1;
      for (let i = 0; i <= 4; i++) {
        const y = pad.t + h * i / 4;
        ctx.beginPath(); ctx.moveTo(pad.l, y); ctx.lineTo(cssW - pad.r, y); ctx.stroke();
      }
      if (!points.length) {
        ctx.fillStyle = '#5b6b66'; ctx.font = '14px sans-serif';
        ctx.fillText('ยังไม่มีข้อมูลสำหรับกราฟ', pad.l, pad.t + 40);
        return;
      }
      const slot = w / Math.max(1, points.length);
      const bw = Math.max(5, Math.min(14, slot * .28));
      points.forEach((p, i) => {
        const x = pad.l + i * slot + slot * .22;
        const incH = h * (p.income || 0) / max;
        const expH = h * (p.expense || 0) / max;
        ctx.fillStyle = '#0f8a5f';
        roundRect(ctx, x, pad.t + h - incH, bw, incH, 3); ctx.fill();
        ctx.fillStyle = '#c24136';
        roundRect(ctx, x + bw + 3, pad.t + h - expH, bw, expH, 3); ctx.fill();
      });
      ctx.fillStyle = '#5b6b66'; ctx.font = '11px sans-serif';
      ctx.fillText(points[0]?.label || '', pad.l, cssH - 12);
      ctx.textAlign = 'right';
      ctx.fillText(points[points.length - 1]?.label || '', cssW - pad.r, cssH - 12);
      ctx.textAlign = 'left';
      ctx.fillStyle = '#0f8a5f'; ctx.fillText('■ รายรับ', pad.l, 13);
      ctx.fillStyle = '#c24136'; ctx.fillText('■ รายจ่าย', pad.l + 72, 13);
    }

    function roundRect(ctx, x, y, w, h, r) {
      if (h < 1) h = 1;
      ctx.beginPath();
      ctx.moveTo(x + r, y);
      ctx.arcTo(x + w, y, x + w, y + h, r);
      ctx.arcTo(x + w, y + h, x, y + h, r);
      ctx.arcTo(x, y + h, x, y, r);
      ctx.arcTo(x, y, x + w, y, r);
      ctx.closePath();
    }

    async function sendLineCommand(text) {
      try {
        if (!window.liff || !configuredLiffId) {
          showToast('เปิดในแอป LINE แล้วแตะอีกครั้งครับ');
          return;
        }
        if (!state.liffReady) {
          await liff.init({ liffId: configuredLiffId });
          state.liffReady = true;
        }
        if (!liff.isInClient()) {
          showToast('ปุ่มนี้ใช้ส่งคำสั่งกลับไปที่แชท LINE ต้องเปิดจากในแอป LINE ครับ');
          return;
        }
        await liff.sendMessages([{ type: 'text', text: text }]);
        showToast('ส่งคำสั่ง "' + text + '" ไปที่แชทแล้วครับ');
        setTimeout(() => liff.closeWindow(), 650);
      } catch (err) {
        showToast('ส่งคำสั่งไม่สำเร็จครับ ลองพิมพ์ในแชทว่า ' + text);
      }
    }

    function scrollToId(id) {
      const el = document.getElementById(id);
      if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }

    function showError(text) {
      document.getElementById('notes').innerHTML = '<div class="error">' + text + '</div>';
    }

    function showToast(text) {
      const el = document.getElementById('toast');
      el.textContent = text;
      el.style.display = 'block';
      clearTimeout(window.__toastTimer);
      window.__toastTimer = setTimeout(() => { el.style.display = 'none'; }, 2800);
    }

    window.addEventListener('resize', () => state.data && drawCashflow(state.data.cashflow || []));
    window.addEventListener('focus', () => loadDashboard({ silent: true }));
    window.addEventListener('pageshow', () => loadDashboard({ silent: true }));
    document.addEventListener('visibilitychange', () => {
      if (!document.hidden) loadDashboard({ silent: true });
    });
    updatePresetButtons();
    loadDashboard();
  </script>
</body>
</html>`
