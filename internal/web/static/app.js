// whodar web UI: post the query to /api/ask and render people and channels.
// Result text is set with textContent so indexed data cannot inject markup.

const form = document.getElementById("ask-form");
const qInput = document.getElementById("q");
const modeSel = document.getElementById("mode");
const statusEl = document.getElementById("status");
const summaryEl = document.getElementById("summary");
const peopleSection = document.getElementById("people-section");
const channelsSection = document.getElementById("channels-section");
const peopleEl = document.getElementById("people");
const channelsEl = document.getElementById("channels");
const button = form.querySelector("button");

form.addEventListener("submit", async (event) => {
  event.preventDefault();
  await ask();
});

async function ask() {
  const q = qInput.value.trim();
  if (!q) return;

  button.disabled = true;
  clearResults();
  statusEl.textContent = modeSel.value === "llm" ? "Asking the local model..." : "Searching...";

  try {
    const params = new URLSearchParams({ q, mode: modeSel.value });
    setParam("person", "");
    setParam("q", q);
    const res = await fetch("/api/ask?" + params.toString());
    const data = await res.json();
    if (!res.ok) {
      statusEl.textContent = "Error: " + (data.error || res.statusText);
      return;
    }
    render(data);
  } catch (err) {
    statusEl.textContent = "Request failed: " + err.message;
  } finally {
    button.disabled = false;
  }
}

// setParam updates one query parameter in the address bar without reloading.
function setParam(key, val) {
  const p = new URLSearchParams(location.search);
  if (val) {
    p.set(key, val);
  } else {
    p.delete(key);
  }
  const s = p.toString();
  history.replaceState(null, "", s ? "?" + s : location.pathname);
}

// A shared link carries the question and person in the URL; run them on load.
const linked = new URLSearchParams(location.search);
if (linked.get("q")) {
  qInput.value = linked.get("q");
  ask();
}
if (linked.get("person")) {
  openProfile(linked.get("person"));
}

function clearResults() {
  summaryEl.hidden = true;
  summaryEl.textContent = "";
  peopleEl.replaceChildren();
  channelsEl.replaceChildren();
  peopleSection.hidden = true;
  channelsSection.hidden = true;
}

function render(data) {
  if (data.summary) {
    summaryEl.textContent = data.summary;
    summaryEl.hidden = false;
  }
  const people = data.people || [];
  const channels = data.channels || [];

  for (const p of people) peopleEl.appendChild(personCard(p, data.query));
  for (const c of channels) channelsEl.appendChild(channelCard(c, data.query));
  peopleSection.hidden = people.length === 0;
  channelsSection.hidden = channels.length === 0;

  if (people.length === 0 && channels.length === 0) {
    statusEl.textContent = "No matches. Try different words.";
  } else {
    statusEl.textContent = people.length + " people, " + channels.length + " channels";
  }
}

function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text != null) node.textContent = text;
  return node;
}

function chips(parent, items) {
  if (!items || !items.length) return;
  const wrap = el("div", "chips");
  for (const item of items) wrap.appendChild(el("span", "chip", item));
  parent.appendChild(wrap);
}

function confidenceBadge(c) {
  if (!c) return null;
  const label = c >= 0.75 ? "strong" : c >= 0.45 ? "moderate" : "weak";
  return el("span", "conf conf-" + label, label);
}

function voteButtons(query, target) {
  const wrap = el("div", "votes");
  const note = document.createElement("input");
  note.className = "vote-note";
  note.type = "text";
  note.placeholder = "why? (optional)";
  for (const [label, vote] of [["helpful", "helpful"], ["wrong", "not-helpful"]]) {
    const button = el("button", "vote", label);
    button.type = "button";
    button.addEventListener("click", async () => {
      try {
        const res = await fetch("/api/feedback", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ query, vote, comment: note.value.trim(), ...target }),
        });
        wrap.replaceChildren(el("span", "voted", res.ok ? "thanks" : "failed"));
      } catch (err) {
        wrap.replaceChildren(el("span", "voted", "failed"));
      }
    });
    wrap.appendChild(button);
  }
  wrap.appendChild(note);
  return wrap;
}

function personCard(p, query) {
  const card = el("div", "card");
  const name = el("div", "name");
  const toggle = el("button", "name-toggle", p.name || p.email || "unknown");
  toggle.type = "button";
  toggle.title = "Show everything whodar knows";
  toggle.addEventListener("click", () => openProfile(p.id || p.email));
  name.appendChild(toggle);
  const copyText = ((p.name || "") + (p.email ? " <" + p.email + ">" : "")).trim();
  if (copyText) name.appendChild(copyButton(copyText));
  const badge = confidenceBadge(p.confidence);
  if (badge) name.appendChild(badge);
  card.appendChild(name);

  const sub = [p.title, p.team].filter(Boolean).join(" · ");
  if (sub) card.appendChild(el("div", "sub", sub));
  if (p.email) card.appendChild(el("div", "sub", p.email));
  chips(card, p.reasons);
  if (query && p.id) card.appendChild(voteButtons(query, { person: p.id }));
  return card;
}

async function openProfile(id) {
  if (!id) return;
  try {
    const res = await fetch("/api/person?id=" + encodeURIComponent(id));
    if (!res.ok) return;
    showProfile(await res.json());
  } catch (err) {
    // A failed lookup just leaves the page as it is.
  }
}

function showProfile(p) {
  closeProfile();
  const backdrop = el("div", "modal-backdrop");
  backdrop.id = "profile-modal";
  backdrop.addEventListener("click", (event) => {
    if (event.target === backdrop) closeProfile();
  });
  const modal = el("div", "modal");

  const name = el("div", "name", p.name || p.id);
  const close = el("button", "modal-close", "close");
  close.type = "button";
  close.addEventListener("click", closeProfile);
  name.appendChild(close);
  modal.appendChild(name);

  const sub = [p.title, p.team, p.org].filter(Boolean).join(" · ");
  if (sub) modal.appendChild(el("div", "sub", sub));

  const rows = el("div", "details");
  const row = (label, value) => {
    const r = el("div", "detail-row");
    r.appendChild(el("span", "detail-label", label));
    r.appendChild(value);
    rows.appendChild(r);
  };
  if (p.email) {
    const v = el("span", "detail-value", p.email);
    v.appendChild(copyButton(p.email));
    const mail = el("a", "detail-action", "email");
    mail.href = "mailto:" + p.email;
    v.appendChild(mail);
    row("Email", v);
  }
  if (p.id && p.id !== p.email) row("Id", el("span", "detail-value", p.id));
  if (p.identities && p.identities.length) {
    row("Also known as", el("span", "detail-value", p.identities.join(", ")));
  }
  if (p.manager && (p.manager.name || p.manager.email)) {
    row("Manager", el("span", "detail-value", p.manager.name || p.manager.email));
  }
  if (p.channels && p.channels.length) {
    row("Active in", el("span", "detail-value", p.channels.map((c) => "#" + c).join(", ")));
  }
  if (p.topics && p.topics.length) {
    const v = el("span", "detail-value detail-chips");
    for (const topic of p.topics) v.appendChild(el("span", "chip", topic));
    row("Knows about", v);
  }
  modal.appendChild(rows);
  backdrop.appendChild(modal);
  document.body.appendChild(backdrop);
  setParam("person", p.id);
}

function closeProfile() {
  const open = document.getElementById("profile-modal");
  if (open) {
    open.remove();
    setParam("person", "");
  }
}

document.addEventListener("keydown", (event) => {
  if (event.key === "Escape") closeProfile();
});

function channelCard(c, query) {
  const card = el("div", "card");
  const name = el("div", "name", "#" + c.name);
  name.appendChild(copyButton("#" + c.name));
  const badge = confidenceBadge(c.confidence);
  if (badge) name.appendChild(badge);
  card.appendChild(name);
  if (c.topic) card.appendChild(el("div", "sub", c.topic));

  const members = c.members || [];
  if (members.length) {
    const sub = el("div", "sub");
    sub.appendChild(document.createTextNode("Active: "));
    members.forEach((m, i) => {
      if (i) sub.appendChild(document.createTextNode(", "));
      const span = el("span", "member", m.name || m.email || "");
      if (m.email) span.title = m.email;
      sub.appendChild(span);
    });
    card.appendChild(sub);
  }
  chips(card, c.reasons);
  if (query && c.name) card.appendChild(voteButtons(query, { channel: c.name }));
  return card;
}

function copyButton(text) {
  const button = el("button", "copy", "copy");
  button.type = "button";
  button.addEventListener("click", async () => {
    try {
      await navigator.clipboard.writeText(text);
      button.textContent = "copied";
      setTimeout(() => (button.textContent = "copy"), 1200);
    } catch (err) {
      button.textContent = "failed";
    }
  });
  return button;
}
