package main

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"
)

const browserGrace = 20 * time.Second

func (s *receiver) localRender(w http.ResponseWriter, page string, data any) {
	script := `<input type="hidden" id="source-token" value="` + template.HTMLEscapeString(s.token) + `"><p id="browser-lease-status" style="font:14px sans-serif;text-align:center"></p><script src="/local.js"></script>`
	if strings.Contains(page, "</div></main>") {
		page = strings.Replace(page, "</div></main>", script+"</div></main>", 1)
	} else {
		page = strings.Replace(page, "</body>", script+"</body>", 1)
	}
	render(w, page, data)
}
func (s *receiver) browserScript(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/javascript")
	w.Write([]byte(localHeartbeatJS))
}

func (s *receiver) reopenBrowser(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
		http.Error(w, "Session changed", 403)
		return
	}
	if !s.finishMu.TryLock() {
		http.Error(w, "Session is finishing", 409)
		return
	}
	defer s.finishMu.Unlock()
	s.mu.Lock()
	finishing := s.finishing
	s.mu.Unlock()
	if finishing || s.finished {
		http.Error(w, "Session is finishing", 409)
		return
	}
	s.leaseMu.Lock()
	s.browserFirst, s.browserLast = time.Time{}, time.Time{}
	s.browserArmed = false
	s.leaseMu.Unlock()
	w.Write([]byte("ready"))
}

const localHeartbeatJS = `(function(){
 var token=document.getElementById('source-token'),label=document.getElementById('browser-lease-status'),files=document.getElementById('session-files'),revision=files?files.getAttribute('data-revision'):null,json=window.JSON&&window.JSON.parse;if(!token||!window.XMLHttpRequest)return;
 function text(node,value){while(node.firstChild)node.removeChild(node.firstChild);node.appendChild(document.createTextNode(value));}
 function child(parent,tag,cls,value){var node=document.createElement(tag);if(cls)node.className=cls;if(value!==undefined)text(node,value);parent.appendChild(node);return node;}
 function session(data){if(!files||String(data.revision)===revision)return;revision=String(data.revision);text(files,'');var list=data.files||[];if(!list.length)child(files,'p','session-empty','Waiting for files. This list updates automatically.');for(var i=0;i<list.length;i++){var f=list[i],row=child(files,'div','session-file');child(row,'span','session-type',f.type);child(row,'strong','',f.name);child(row,'div','session-meta','Received · '+f.sizeLabel+' · '+f.time);}var summary=document.getElementById('received-summary');if(summary)text(summary,data.count+' file'+(data.count===1?'':'s')+' received · '+data.totalLabel);var all=document.getElementById('all-received');if(all){all.style.display=data.count>3?'block':'none';all.removeAttribute('hidden');}var sharing=document.getElementById('source-sharing');if(sharing)text(sharing,'Sharing: '+data.sharing);}
 function beat(){var xhr=new XMLHttpRequest(),completed=false;
 function next(){if(completed)return;completed=true;window.setTimeout(beat,3000);}
 xhr.onreadystatechange=function(){if(xhr.readyState===4){if(xhr.status===200){var state=xhr.responseText;if(json){try{var data=window.JSON.parse(xhr.responseText);state=data.state;session(data);}catch(e){state='manual';}}if(state==='reload'){window.location.reload();return;}if(label)label.innerHTML=state==='manual'?'Use Finish &amp; refresh library when done.':state==='armed'?'Closing this window finishes and refreshes the library after 20 seconds.':'Preparing automatic finish…';}else if(label)label.innerHTML='Use Finish &amp; refresh library when done.';next();}};
 try{xhr.open('POST','/local-heartbeat',true);xhr.setRequestHeader('Content-Type','application/x-www-form-urlencoded');var source=document.getElementById('source-address');xhr.send('token='+encodeURIComponent(token.value)+(json?'&format=json':'')+(source?'&source='+encodeURIComponent(source.value):''));}catch(e){next();}
 window.setTimeout(function(){if(!completed){try{xhr.abort();}catch(e){}next();}},5000);
 }beat();})();`

func (s *receiver) heartbeat(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		http.NotFound(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	if r.ParseForm() != nil || !equal(r.PostForm.Get("token"), s.token) {
		http.Error(w, "Session changed", 403)
		return
	}
	if s.autoFinish {
		s.noteBrowser(time.Now())
	}
	s.netMu.RLock()
	source := s.url + "|" + s.fallbackURL
	pending := strings.HasPrefix(s.mdnsStatus, "Checking")
	s.netMu.RUnlock()
	s.leaseMu.Lock()
	armed := s.browserArmed
	s.leaseMu.Unlock()
	state := "starting"
	if armed {
		state = "armed"
	}
	if !s.autoFinish {
		state = "manual"
	}
	if expected := r.PostForm.Get("source"); expected != "" && expected != source && !pending {
		state = "reload"
	}
	if r.PostForm.Get("format") == "json" {
		count, total, recent := s.receivedSnapshot(3)
		writeJSON(w, map[string]any{"state": state, "revision": count, "count": count, "totalLabel": humanSize(total), "files": recent, "sharing": s.sharingSummary()})
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(state))
}
func (s *receiver) noteBrowser(now time.Time) {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if s.browserFirst.IsZero() || now.Sub(s.browserLast) > browserGrace {
		s.browserFirst = now
		s.browserArmed = false
	}
	s.browserLast = now
	if now.Sub(s.browserFirst) >= 2*time.Second {
		s.browserArmed = true
	}
}
func (s *receiver) browserExpired(now time.Time) bool {
	s.leaseMu.Lock()
	defer s.leaseMu.Unlock()
	if !s.browserArmed || now.Sub(s.browserLast) < browserGrace {
		return false
	}
	s.browserArmed = false
	s.browserFirst = time.Time{}
	return true
}
func (s *receiver) watchBrowser(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Time values retain Linux CLOCK_MONOTONIC readings: suspend time does not
			// consume the grace period. A resumed browser has time to send its next beat.
			s.finishMu.Lock()
			if s.browserExpired(time.Now()) {
				log.Print("Local browser heartbeat stopped; finishing and refreshing library")
				if e := s.finishTransfersLocked(); e != nil {
					log.Printf("Automatic library refresh failed; receiving remains active: %v", e)
				}
			}
			s.finishMu.Unlock()
		}
	}
}
