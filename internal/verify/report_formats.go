package verify

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func writeMarkdown(w io.Writer, rep *Report) error {
	if w == nil || rep == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("# ktl verify report\n\n")
	b.WriteString(fmt.Sprintf("- Evaluated: %s\n", rep.EvaluatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Mode: %s\n", rep.Mode))
	b.WriteString(fmt.Sprintf("- Passed: %t\n", rep.Passed))
	b.WriteString(fmt.Sprintf("- Blocked: %t\n", rep.Blocked))
	if strings.TrimSpace(rep.Engine.Ruleset) != "" {
		b.WriteString(fmt.Sprintf("- Ruleset: %s\n", strings.TrimSpace(rep.Engine.Ruleset)))
	}
	b.WriteString(fmt.Sprintf("- Total findings: %d\n", rep.Summary.Total))

	if len(rep.Summary.BySev) > 0 {
		b.WriteString("\n## By severity\n\n")
		writeMarkdownSeverityTable(&b, rep.Summary.BySev)
	}

	if len(rep.Findings) == 0 {
		b.WriteString("\n_No findings._\n")
		_, _ = w.Write([]byte(b.String()))
		return nil
	}

	b.WriteString("\n## Findings\n\n")
	b.WriteString("| Severity | Rule | Resource | Message | Location | Expected | Observed | Help |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range rep.Findings {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			strings.ToUpper(string(f.Severity)),
			mdEscape(f.RuleID),
			mdEscape(findingResource(f)),
			mdEscape(f.Message),
			mdEscape(findingLocation(f)),
			mdEscape(f.Expected),
			mdEscape(f.Observed),
			mdEscape(f.HelpURL),
		))
	}
	_, _ = w.Write([]byte(b.String()))
	return nil
}

func writeMarkdownSeverityTable(b *strings.Builder, bySev map[Severity]int) {
	if b == nil {
		return
	}
	sevs := make([]Severity, 0, len(bySev))
	for sev := range bySev {
		sevs = append(sevs, sev)
	}
	sort.Slice(sevs, func(i, j int) bool { return severityRank(sevs[i]) < severityRank(sevs[j]) })
	b.WriteString("| Severity | Count |\n")
	b.WriteString("| --- | --- |\n")
	for _, sev := range sevs {
		b.WriteString(fmt.Sprintf("| %s | %d |\n", strings.ToUpper(string(sev)), bySev[sev]))
	}
}

func writeHTML(w io.Writer, rep *Report) error {
	if w == nil || rep == nil {
		return nil
	}

	type htmlPayload struct {
		Report  *Report     `json:"report"`
		FixPlan []FixChange `json:"fixPlan,omitempty"`
	}
	payload := htmlPayload{Report: rep, FixPlan: BuildFixPlan(rep.Findings)}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// Avoid closing the script tag if report content contains "</script>"-like sequences.
	jsonText := strings.ReplaceAll(string(raw), "</", "<\\/")

	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\" />\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n")
	b.WriteString("<title>ktl verify report</title>\n")
	b.WriteString("<style>\n")
	// Lux minimalist: ivory surfaces, deep ink text, brass accents.
	b.WriteString(":root{color-scheme:light;--ink:#0b1f2a;--muted:rgba(11,31,42,0.62);--navy:#0b3a53;--brass:#b08d57;--surface:rgba(255,255,255,0.92);--surface-soft:rgba(255,255,255,0.84);--border:rgba(11,31,42,0.12);--shadow:0 26px 60px rgba(11,31,42,0.12);--shadow-soft:0 16px 34px rgba(11,31,42,0.10);--ease:cubic-bezier(.16,1,.3,1);} \n")
	b.WriteString("*{box-sizing:border-box;}body{margin:0;padding:44px 52px 72px;font-family:\"SF Pro Display\",\"SF Pro Text\",-apple-system,BlinkMacSystemFont,\"Segoe UI\",Roboto,sans-serif;background:radial-gradient(1200px 700px at 18% 12%, #ffffff 0%, #eef2f7 38%, #dce3f1 100%);color:var(--ink);}h1{margin:0;font-size:2.3rem;letter-spacing:-0.03em;font-weight:650;}h2{margin:1.2rem 0 0.6rem;font-size:1.05rem;letter-spacing:0.01em;}p{margin:0.25rem 0;color:var(--muted);}a{color:var(--navy);text-decoration:none;}a:hover{text-decoration:underline;} \n")
	b.WriteString(".chrome{max-width:1280px;margin:0 auto;} \n")
	b.WriteString(".panel{background:var(--surface);border:1px solid var(--border);border-radius:26px;padding:26px 28px;box-shadow:var(--shadow);backdrop-filter:blur(18px);} \n")
	b.WriteString(".panel.soft{background:var(--surface-soft);box-shadow:var(--shadow-soft);} \n")
	b.WriteString(".layout{display:flex;gap:18px;align-items:flex-start;margin-top:16px;} \n")
	b.WriteString("aside{flex:0 0 320px;position:sticky;top:18px;}main{flex:1 1 auto;min-width:0;} \n")
	b.WriteString(".hero{display:flex;gap:16px;align-items:flex-end;justify-content:space-between;flex-wrap:wrap;} \n")
	b.WriteString(".subtitle{font-size:0.98rem;color:var(--muted);} \n")
	b.WriteString(".metrics{display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:12px;margin-top:14px;} \n")
	b.WriteString(".metric{padding:14px 14px;border:1px solid rgba(11,31,42,0.10);border-radius:18px;background:rgba(255,255,255,0.55);} \n")
	b.WriteString(".metric .k{font-size:0.72rem;text-transform:uppercase;letter-spacing:0.18em;color:var(--muted);font-weight:650;} \n")
	b.WriteString(".metric .v{margin-top:6px;font-size:1.1rem;font-weight:680;overflow-wrap:anywhere;word-break:break-word;} \n")
	b.WriteString(".toolbar{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin-top:14px;} \n")
	b.WriteString(".search{flex:1 1 320px;display:flex;align-items:center;gap:8px;background:rgba(255,255,255,0.65);border:1px solid var(--border);border-radius:999px;padding:10px 14px;box-shadow:0 0 0 rgba(0,0,0,0);} \n")
	b.WriteString(".search:focus-within{box-shadow:0 0 0 4px rgba(11,58,83,0.20);border-color:rgba(11,58,83,0.45);} \n")
	b.WriteString(".search input{border:0;outline:0;background:transparent;width:100%;font-size:0.95rem;color:var(--ink);} \n")
	b.WriteString(".chips{display:flex;gap:8px;flex-wrap:wrap;} \n")
	b.WriteString(".chip{border-radius:999px;padding:8px 12px;border:1px solid var(--border);background:rgba(255,255,255,0.55);color:var(--navy);font-size:0.72rem;text-transform:uppercase;letter-spacing:0.16em;font-weight:650;cursor:pointer;transition:transform 140ms var(--ease),box-shadow 140ms var(--ease),background 140ms var(--ease),border-color 140ms var(--ease);} \n")
	b.WriteString(".chip[data-on='1']{box-shadow:0 0 0 2px rgba(176,141,87,0.35);border-color:rgba(176,141,87,0.55);background:rgba(176,141,87,0.10);transform:translateY(-1px);} \n")
	b.WriteString(".chip.sev-critical,.chip.sev-high{color:#7f1d1d;} .chip.sev-medium{color:#7c4a00;} .chip.sev-low{color:#0c4a6e;} .chip.sev-info{color:rgba(11,31,42,0.70);} \n")
	b.WriteString("select,button,input{font-family:inherit;} \n")
	b.WriteString(".select{border:1px solid var(--border);border-radius:999px;background:rgba(255,255,255,0.55);padding:9px 12px;font-size:0.9rem;color:var(--ink);} \n")
	b.WriteString(".tabs{display:flex;gap:8px;flex-wrap:wrap;margin-top:10px;} \n")
	b.WriteString(".tab{border-radius:999px;padding:8px 12px;border:1px solid var(--border);background:rgba(255,255,255,0.55);cursor:pointer;font-size:0.78rem;letter-spacing:0.14em;text-transform:uppercase;font-weight:650;color:var(--ink);} \n")
	b.WriteString(".tab[data-on='1']{border-color:rgba(176,141,87,0.55);box-shadow:0 0 0 2px rgba(176,141,87,0.30);background:rgba(176,141,87,0.10);} \n")
	b.WriteString(".table{width:100%;border-collapse:collapse;margin-top:10px;font-size:0.95rem;} \n")
	b.WriteString(".table th,.table td{padding:10px 10px;border-bottom:1px solid rgba(11,31,42,0.08);vertical-align:top;text-align:left;} \n")
	b.WriteString(".table th{font-size:0.72rem;text-transform:uppercase;letter-spacing:0.18em;color:var(--muted);font-weight:680;} \n")
	b.WriteString(".mono{font-family:\"SFMono-Regular\",\"JetBrains Mono\",\"Menlo\",\"Source Code Pro\",monospace;font-size:0.9em;overflow-wrap:anywhere;word-break:break-word;} \n")
	b.WriteString(".badge{display:inline-flex;align-items:center;gap:8px;border-radius:999px;padding:4px 10px;font-size:0.72rem;text-transform:uppercase;letter-spacing:0.16em;font-weight:700;border:1px solid rgba(11,31,42,0.10);background:rgba(255,255,255,0.55);} \n")
	b.WriteString(".dot{width:8px;height:8px;border-radius:999px;background:rgba(11,31,42,0.35);} \n")
	b.WriteString(".sev-critical .dot,.sev-high .dot{background:#ef4444;} .sev-medium .dot{background:#f59e0b;} .sev-low .dot{background:#0ea5e9;} .sev-info .dot{background:rgba(11,31,42,0.35);} \n")
	b.WriteString(".row{cursor:pointer;} .row:hover{background:rgba(11,31,42,0.03);} \n")
	b.WriteString(".klist{list-style:none;padding:0;margin:10px 0 0;} .klist li{display:flex;justify-content:space-between;gap:10px;padding:8px 0;border-bottom:1px solid rgba(11,31,42,0.07);} .klist li:last-child{border-bottom:none;} \n")
	b.WriteString(".muted{color:var(--muted);} \n")
	b.WriteString(".toast{position:fixed;left:50%;bottom:22px;transform:translateX(-50%);background:rgba(11,31,42,0.92);color:#fff;padding:10px 14px;border-radius:999px;font-size:0.9rem;opacity:0;pointer-events:none;transition:opacity 160ms var(--ease);} .toast.on{opacity:1;} \n")
	b.WriteString(".drawer-backdrop{position:fixed;inset:0;background:rgba(11,31,42,0.28);opacity:0;pointer-events:none;transition:opacity 160ms var(--ease);} .drawer-backdrop.on{opacity:1;pointer-events:auto;} \n")
	b.WriteString(".drawer{position:fixed;top:0;right:0;height:100%;width:min(520px, 92vw);background:rgba(255,255,255,0.96);border-left:1px solid rgba(11,31,42,0.10);box-shadow:0 40px 90px rgba(11,31,42,0.22);transform:translateX(100%);transition:transform 220ms var(--ease);padding:20px 18px 24px;overflow:auto;} .drawer.on{transform:translateX(0);} \n")
	b.WriteString(".drawer h2{margin-top:0.25rem;} \n")
	b.WriteString(".btn{border-radius:999px;padding:9px 12px;border:1px solid rgba(11,31,42,0.16);background:rgba(255,255,255,0.70);cursor:pointer;font-weight:650;} .btn:hover{border-color:rgba(176,141,87,0.60);} \n")
	b.WriteString("pre{white-space:pre-wrap;margin:10px 0 0;background:rgba(11,31,42,0.04);border:1px solid rgba(11,31,42,0.08);border-radius:16px;padding:12px 14px;overflow-wrap:anywhere;word-break:break-word;} \n")
	b.WriteString("body.print .toolbar,body.print aside,body.print .drawer,body.print .drawer-backdrop,body.print .toast{display:none !important;} body.print .layout{display:block;} body.print .panel{box-shadow:none;} \n")
	b.WriteString("@media (max-width:1100px){body{padding:26px 16px 48px;} aside{position:relative;top:auto;flex:1 1 100%;} .layout{flex-direction:column;} } \n")
	b.WriteString("</style>\n</head>\n<body>\n<div class=\"chrome\">\n")

	b.WriteString("<div class=\"panel\" id=\"hero\"></div>\n")
	b.WriteString("<div class=\"layout\">\n")
	b.WriteString("<aside class=\"panel soft\" id=\"sidebar\"></aside>\n")
	b.WriteString("<main>\n")
	b.WriteString("<div class=\"panel\" id=\"main\"></div>\n")
	b.WriteString("</main>\n")
	b.WriteString("</div>\n")
	b.WriteString("</div>\n")

	b.WriteString("<div class=\"drawer-backdrop\" id=\"drawerBackdrop\"></div>\n")
	b.WriteString("<div class=\"drawer\" id=\"drawer\"></div>\n")
	b.WriteString("<div class=\"toast\" id=\"toast\">Copied</div>\n")

	b.WriteString("<script type=\"application/json\" id=\"ktlVerifyData\">")
	b.WriteString(jsonText)
	b.WriteString("</script>\n")

	b.WriteString("<script>\n")
	b.WriteString(strings.TrimSpace(verifyHTMLJS))
	b.WriteString("\n</script>\n")

	b.WriteString("</body>\n</html>\n")
	_, _ = io.WriteString(w, b.String())
	return nil
}

func mdEscape(value string) string {
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

func findingResource(f Finding) string {
	if f.ResourceKey != "" {
		return f.ResourceKey
	}
	return resourceKey(f.Subject)
}

func findingLocation(f Finding) string {
	if f.Path != "" {
		return f.Path
	}
	return f.Location
}

const verifyHTMLJS = `
(function(){
  var raw = document.getElementById('ktlVerifyData');
  if(!raw){ return; }
  var data = {};
  try { data = JSON.parse(raw.textContent || '{}'); } catch(e) { data = {}; }
  var rep = (data && data.report) || {};
  var fixPlan = (data && data.fixPlan) || [];

  function qs(sel){ return document.querySelector(sel); }
  function ce(tag, cls){ var el=document.createElement(tag); if(cls){ el.className=cls; } return el; }
  function text(el, s){ el.textContent = (s==null?'':String(s)); return el; }
  function fmtRFC3339(s){ return s || ''; }
  function uniq(arr){ var m={}; var out=[]; (arr||[]).forEach(function(v){ var k=String(v); if(!m[k]){ m[k]=1; out.push(v); } }); return out; }
  function sevKey(s){ return (s||'info').toLowerCase(); }
  function sevRank(s){ s=sevKey(s); if(s==='critical')return 0; if(s==='high')return 1; if(s==='medium')return 2; if(s==='low')return 3; return 4; }
  function badgeFor(sev){
    var s = sevKey(sev);
    var span = ce('span', 'badge sev-'+s);
    var dot = ce('span','dot');
    span.appendChild(dot);
    span.appendChild(text(ce('span'), s.toUpperCase()));
    return span;
  }
  function resourceKey(f){
    if(f.resourceKey){ return f.resourceKey; }
    var k = (f.subject && f.subject.kind) ? String(f.subject.kind) : '';
    var ns = (f.subject && f.subject.namespace) ? String(f.subject.namespace) : '';
    var nm = (f.subject && f.subject.name) ? String(f.subject.name) : '';
    var out = '';
    if(ns){ out += ns + '/'; }
    if(k){ out += k + '/'; }
    out += nm || '-';
    return out;
  }
  function fieldKey(f){
    var loc = (f.location || '').trim();
    var path = (f.path || '').trim();
    var base = path || loc;
    if(f.line && f.line > 0){
      if(base){ return base + ':' + f.line; }
      return String(f.line);
    }
    return base;
  }
  function isPolicy(f){ return String(f.ruleId||'').indexOf('policy/')===0; }
  function topNFromMap(m, n){
    var items=[];
    for(var k in (m||{})){ items.push({k:k, v:m[k]}); }
    items.sort(function(a,b){ if(b.v!==a.v) return b.v-a.v; return a.k.localeCompare(b.k); });
    return items.slice(0,n||5);
  }
  function toast(msg){
    var t = qs('#toast');
    if(!t) return;
    t.textContent = msg || 'Copied';
    t.classList.add('on');
    window.clearTimeout(toast._tid);
    toast._tid = window.setTimeout(function(){ t.classList.remove('on'); }, 1200);
  }
  async function copyText(s){
    try{
      await navigator.clipboard.writeText(String(s||''));
      toast('Copied');
      return true;
    }catch(e){
      try{
        var ta=ce('textarea'); ta.value=String(s||''); document.body.appendChild(ta); ta.select(); document.execCommand('copy'); document.body.removeChild(ta);
        toast('Copied');
        return true;
      }catch(e2){
        toast('Copy failed');
        return false;
      }
    }
  }

  function applyPrintMode(){
    var sp = new URLSearchParams(location.search||'');
    if(sp.has('print')){
      document.body.classList.add('print');
    }
  }

  function normalizeFindings(list){
    return (list||[]).map(function(f){
      var out = Object.assign({}, f);
      out._sev = sevKey(out.severity);
      out._rule = String(out.ruleId || '').trim();
      out._res = resourceKey(out);
      out._ns = (out.subject && out.subject.namespace) ? String(out.subject.namespace) : '';
      out._field = fieldKey(out);
      out._src = isPolicy(out) ? 'policy' : 'rules';
      out._fp = String(out.fingerprint || (out._rule + ':' + out._res + ':' + out._field)).trim();
      out._search = (out._rule + ' ' + out._res + ' ' + (out.message||'') + ' ' + (out.category||'') + ' ' + out._field).toLowerCase();
      return out;
    });
  }

  var allFindings = normalizeFindings(rep.findings || []);
  var delta = rep.delta || null;
  var deltaNew = delta && delta.newOrChanged ? normalizeFindings(delta.newOrChanged) : null;
  var deltaFixed = delta && delta.fixed ? normalizeFindings(delta.fixed) : null;

  // When compare-to is used, rep.findings is new/changed already. "All" should mean baseline-aware:
  // show new/changed + fixed, and also allow the user to switch tabs.
  var views = [];
  if(delta){
    views = [
      {id:'new', label:'New/Changed', list: deltaNew || allFindings},
      {id:'fixed', label:'Fixed', list: deltaFixed || []},
      {id:'all', label:'All', list: (deltaNew||[]).concat(deltaFixed||[])}
    ];
  }else{
    views = [{id:'all', label:'All', list: allFindings}];
  }

  var state = {
    q: '',
    view: (delta ? 'new' : 'all'),
    groupBy: 'none',
    sev: {critical:true, high:true, medium:true, low:true, info:true},
    src: {rules:true, policy:true},
    ns: 'all',
    hideDup: false
  };

  function renderHero(){
    var hero = qs('#hero'); if(!hero) return;
    hero.innerHTML = '';
    var wrap = ce('div','hero');
    var left = ce('div');
    left.appendChild(text(ce('h1'), 'ktl verify report'));
    left.appendChild(text(ce('div','subtitle'), 'Evaluated: ' + fmtRFC3339(rep.evaluatedAt)));
    wrap.appendChild(left);
    var right = ce('div','subtitle');
    var ruleset = (rep.engine && rep.engine.ruleset) ? rep.engine.ruleset : '';
    text(right, ruleset ? ('Ruleset: ' + ruleset) : '');
    wrap.appendChild(right);
    hero.appendChild(wrap);

    var metrics = ce('div','metrics');
    function metric(k,v){
      var m=ce('div','metric');
      m.appendChild(text(ce('div','k'), k));
      m.appendChild(text(ce('div','v'), v));
      return m;
    }
    metrics.appendChild(metric('Mode', rep.mode || '-'));
    metrics.appendChild(metric('Fail On', rep.failOn || '-'));
    metrics.appendChild(metric('Passed', String(!!rep.passed)));
    metrics.appendChild(metric('Blocked', String(!!rep.blocked)));
    metrics.appendChild(metric('Total', String((rep.summary && rep.summary.total) || 0)));
    if(delta){
      metrics.appendChild(metric('Baseline', String(delta.baselineTotal || 0)));
      metrics.appendChild(metric('Unchanged', String(delta.unchanged || 0)));
    }
    hero.appendChild(metrics);
  }

  function renderSidebar(){
    var sb = qs('#sidebar'); if(!sb) return;
    sb.innerHTML = '';
    sb.appendChild(text(ce('h2'), 'Summary'));
    var bySev = (rep.summary && rep.summary.bySeverity) || {};
    var sevLine = ce('div','klist');
    var ul = ce('ul','klist');
    ['critical','high','medium','low','info'].forEach(function(s){
      var li = ce('li');
      var l = ce('span','mono');
      l.appendChild(badgeFor(s));
      li.appendChild(l);
      li.appendChild(text(ce('span','mono'), String(bySev[s] || 0)));
      ul.appendChild(li);
    });
    sb.appendChild(ul);

    sb.appendChild(text(ce('h2'), 'Top Rules'));
    var topRules = topNFromMap((rep.summary && rep.summary.byRule) || {}, 6);
    var ulr = ce('ul','klist');
    topRules.forEach(function(it){
      var li=ce('li'); li.appendChild(text(ce('span','mono'), it.k)); li.appendChild(text(ce('span','mono'), it.v)); ulr.appendChild(li);
    });
    if(topRules.length===0){ ulr.appendChild(text(ce('li','muted'), 'No data')); }
    sb.appendChild(ulr);

    sb.appendChild(text(ce('h2'), 'Inputs'));
    var inps = rep.inputs || [];
    if(inps.length===0){
      sb.appendChild(text(ce('p','muted'), 'No inputs recorded.'));
    }else{
      var uli = ce('ul','klist');
      inps.forEach(function(x){
        var li=ce('li');
        var left = ce('span');
        var kind = String(x.kind||'-');
        var src = (x.source ? (' ' + x.source) : '');
        left.appendChild(text(ce('span','mono'), kind + src));
        li.appendChild(left);
        li.appendChild(text(ce('span','mono'), (x.renderedSha256||'').slice(0,12) || '-'));
        uli.appendChild(li);
      });
      sb.appendChild(uli);
    }
  }

  function findingsForState(){
    var list = null;
    for(var i=0;i<views.length;i++){
      if(views[i].id===state.view){ list = views[i].list; break; }
    }
    if(!list){ list = allFindings; }
    var term = (state.q||'').toLowerCase().trim();
    var out = [];
    var seen = {};
    for(var j=0;j<list.length;j++){
      var f=list[j];
      if(!state.sev[f._sev]) continue;
      if(!state.src[f._src]) continue;
      if(state.ns!=='all' && f._ns!==state.ns) continue;
      if(term && f._search.indexOf(term)===-1) continue;
      if(state.hideDup){
        if(seen[f._fp]) continue;
        seen[f._fp]=1;
      }
      out.push(f);
    }
    out.sort(function(a,b){
      var ra=sevRank(a._sev), rb=sevRank(b._sev);
      if(ra!==rb) return ra-rb;
      if(a._rule!==b._rule) return a._rule.localeCompare(b._rule);
      return a._res.localeCompare(b._res);
    });
    return out;
  }

  function group(list){
    var key = state.groupBy || 'none';
    if(key==='none'){ return [{k:'', list:list}]; }
    var m = {};
    list.forEach(function(f){
      var g='';
      if(key==='rule') g=f._rule;
      else if(key==='resource') g=f._res;
      else if(key==='namespace') g=f._ns || '(cluster)';
      else if(key==='category') g=(f.category||'(uncategorized)');
      if(!m[g]) m[g]=[];
      m[g].push(f);
    });
    var keys = Object.keys(m);
    keys.sort(function(a,b){ return a.localeCompare(b); });
    return keys.map(function(k){ return {k:k, list:m[k]}; });
  }

  function openDrawer(f){
    var bd = qs('#drawerBackdrop'); var dr = qs('#drawer');
    if(!bd || !dr){ return; }
    dr.innerHTML = '';
    var top = ce('div','hero');
    var title = ce('div');
    title.appendChild(text(ce('h2'), (f._rule || 'finding')));
    title.appendChild(text(ce('div','subtitle'), (f._res || '') + (f._field ? (' · ' + f._field) : '')));
    top.appendChild(title);
    var actions = ce('div','chips');
    var b1 = ce('button','btn'); text(b1,'Copy Rule'); b1.onclick=function(){ copyText(f._rule); };
    var b2 = ce('button','btn'); text(b2,'Copy Resource'); b2.onclick=function(){ copyText(f._res); };
    var b3 = ce('button','btn'); text(b3,'Copy Fingerprint'); b3.onclick=function(){ copyText(f._fp); };
    actions.appendChild(b1); actions.appendChild(b2); actions.appendChild(b3);
    top.appendChild(actions);
    dr.appendChild(top);

    var sev = ce('p'); sev.appendChild(badgeFor(f._sev)); dr.appendChild(sev);
    if(f.message){ dr.appendChild(text(ce('p'), f.message)); }
    if(f.expected){ dr.appendChild(text(ce('div','muted'), 'Expected')); var pre=ce('pre','mono'); pre.textContent=String(f.expected); dr.appendChild(pre); }
    if(f.observed){ dr.appendChild(text(ce('div','muted'), 'Observed')); var pre2=ce('pre','mono'); pre2.textContent=String(f.observed); dr.appendChild(pre2); }
    if(f.helpUrl){ var p=ce('p'); var a=ce('a'); a.href=f.helpUrl; a.target='_blank'; a.rel='noreferrer'; a.textContent='Docs'; p.appendChild(a); dr.appendChild(p); }

    // Fix plan match (best effort).
    var fixes = fixPlan.filter(function(x){
      var rk = (String(x.ruleId||'').trim() === String(f.ruleId||'').trim());
      var kk = (String(x.kind||'').trim() === String(f.subject && f.subject.kind || '').trim());
      var nn = (String(x.name||'').trim() === String(f.subject && f.subject.name || '').trim());
      var ns = (String(x.namespace||'').trim() === String(f.subject && f.subject.namespace || '').trim());
      return rk && kk && nn && ns;
    });
    if(fixes.length>0){
      dr.appendChild(text(ce('div','muted'), 'Suggested patch'));
      var pre3=ce('pre','mono'); pre3.textContent=String(fixes[0].patchYaml||''); dr.appendChild(pre3);
      var bc=ce('button','btn'); text(bc,'Copy Patch'); bc.onclick=function(){ copyText(pre3.textContent); }; dr.appendChild(bc);
    }

    bd.classList.add('on');
    dr.classList.add('on');
    location.hash = 'finding=' + encodeURIComponent(f._fp);
  }

  function closeDrawer(){
    var bd = qs('#drawerBackdrop'); var dr = qs('#drawer');
    if(bd) bd.classList.remove('on');
    if(dr) dr.classList.remove('on');
  }

  function renderMain(){
    var main = qs('#main'); if(!main) return;
    main.innerHTML = '';

    var toolbar = ce('div','toolbar');
    var search = ce('div','search');
    var inp = ce('input'); inp.type='search'; inp.placeholder='Search rule, resource, message, field...'; inp.value=state.q || '';
    inp.oninput=function(){ state.q = inp.value; renderMain(); };
    search.appendChild(inp);
    toolbar.appendChild(search);

    var gb = ce('select','select');
    ['none','rule','resource','namespace','category'].forEach(function(k){
      var o=ce('option'); o.value=k; o.textContent='Group: '+k; if(state.groupBy===k){ o.selected=true; } gb.appendChild(o);
    });
    gb.onchange=function(){ state.groupBy = gb.value; renderMain(); };
    toolbar.appendChild(gb);

    var nsSel = ce('select','select');
    var namespaces = uniq(allFindings.map(function(f){ return f._ns; }).filter(function(x){ return x && x.trim(); })).sort();
    var o0=ce('option'); o0.value='all'; o0.textContent='Namespace: all'; nsSel.appendChild(o0);
    namespaces.forEach(function(n){ var o=ce('option'); o.value=n; o.textContent='Namespace: '+n; if(state.ns===n){ o.selected=true; } nsSel.appendChild(o); });
    nsSel.value = state.ns;
    nsSel.onchange=function(){ state.ns = nsSel.value; renderMain(); };
    toolbar.appendChild(nsSel);

    var chips = ce('div','chips');
    ['critical','high','medium','low','info'].forEach(function(s){
      var c=ce('button','chip sev-'+s); c.type='button'; c.setAttribute('data-on', state.sev[s]?'1':'0'); c.textContent=s.toUpperCase();
      c.onclick=function(){ state.sev[s]=!state.sev[s]; c.setAttribute('data-on', state.sev[s]?'1':'0'); renderMain(); };
      chips.appendChild(c);
    });
    ['rules','policy'].forEach(function(s){
      var c=ce('button','chip'); c.type='button'; c.setAttribute('data-on', state.src[s]?'1':'0'); c.textContent=s.toUpperCase();
      c.onclick=function(){ state.src[s]=!state.src[s]; c.setAttribute('data-on', state.src[s]?'1':'0'); renderMain(); };
      chips.appendChild(c);
    });
    var hd=ce('button','chip'); hd.type='button'; hd.setAttribute('data-on', state.hideDup?'1':'0'); hd.textContent='HIDE DUP';
    hd.onclick=function(){ state.hideDup=!state.hideDup; hd.setAttribute('data-on', state.hideDup?'1':'0'); renderMain(); };
    chips.appendChild(hd);
    toolbar.appendChild(chips);
    main.appendChild(toolbar);

    if(views.length > 1){
      var tabs = ce('div','tabs');
      views.forEach(function(v){
        var t=ce('button','tab'); t.type='button'; t.setAttribute('data-on', state.view===v.id?'1':'0');
        var count = (v.list||[]).length;
        t.textContent = v.label + ' ('+count+')';
        t.onclick=function(){ state.view=v.id; renderMain(); };
        tabs.appendChild(t);
      });
      main.appendChild(tabs);
    }

    var list = findingsForState();
    var groups = group(list);

    groups.forEach(function(g){
      if(g.k){
        var h = ce('h2'); text(h, g.k + ' (' + g.list.length + ')'); main.appendChild(h);
      }
      var table = ce('table','table');
      var thead = ce('thead'); var trh = ce('tr');
      ['Severity','Rule','Resource','Message','Field','Help'].forEach(function(n){ var th=ce('th'); th.textContent=n; trh.appendChild(th); });
      thead.appendChild(trh); table.appendChild(thead);
      var tbody = ce('tbody');
      g.list.forEach(function(f){
        var tr=ce('tr','row'); tr.onclick=function(){ openDrawer(f); };
        var td0=ce('td'); td0.appendChild(badgeFor(f._sev)); tr.appendChild(td0);
        tr.appendChild(text(ce('td','mono'), f._rule));
        tr.appendChild(text(ce('td','mono'), f._res));
        tr.appendChild(text(ce('td'), f.message || ''));
        tr.appendChild(text(ce('td','mono'), f._field || ''));
        var td5=ce('td');
        if(f.helpUrl){ var a=ce('a'); a.href=f.helpUrl; a.target='_blank'; a.rel='noreferrer'; a.textContent='docs'; td5.appendChild(a); }
        else { td5.appendChild(text(ce('span','muted'), '-')); }
        tr.appendChild(td5);
        tbody.appendChild(tr);
      });
      table.appendChild(tbody);
      main.appendChild(table);
    });
  }

  function wire(){
    var bd = qs('#drawerBackdrop');
    if(bd){ bd.onclick=closeDrawer; }
    window.addEventListener('keydown', function(e){ if(e.key==='Escape'){ closeDrawer(); } });

    // Deep link: #finding=<fp>
    function tryOpenFromHash(){
      var h = String(location.hash||'').replace(/^#/, '');
      if(h.indexOf('finding=')!==0) return;
      var fp = decodeURIComponent(h.slice('finding='.length));
      var all = allFindings.concat(deltaFixed||[]);
      for(var i=0;i<all.length;i++){
        if(all[i]._fp===fp){ openDrawer(all[i]); return; }
      }
    }
    tryOpenFromHash();
  }

  applyPrintMode();
  renderHero();
  renderSidebar();
  renderMain();
  wire();
})();
`
