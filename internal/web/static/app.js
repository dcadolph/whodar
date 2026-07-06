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
    history.replaceState(null, "", "?q=" + encodeURIComponent(q));
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

// A shared link carries the question in the URL; run it on load.
const linkedQuery = new URLSearchParams(location.search).get("q");
if (linkedQuery) {
  qInput.value = linkedQuery;
  ask();
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

  for (const p of people) peopleEl.appendChild(personCard(p, data.query, people.length === 1));
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
  for (const [label, vote] of [["helpful", "helpful"], ["wrong", "not-helpful"]]) {
    const button = el("button", "vote", label);
    button.type = "button";
    button.addEventListener("click", async () => {
      try {
        const res = await fetch("/api/feedback", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ query, vote, ...target }),
        });
        wrap.replaceChildren(el("span", "voted", res.ok ? "thanks" : "failed"));
      } catch (err) {
        wrap.replaceChildren(el("span", "voted", "failed"));
      }
    });
    wrap.appendChild(button);
  }
  return wrap;
}

function personCard(p, query, expand) {
  const card = el("div", "card");
  const name = el("div", "name");
  const toggle = el("button", "name-toggle", p.name || p.email || "unknown");
  toggle.type = "button";
  toggle.title = "Show details";
  name.appendChild(toggle);
  const copyText = ((p.name || "") + (p.email ? " <" + p.email + ">" : "")).trim();
  if (copyText) name.appendChild(copyButton(copyText));
  const badge = confidenceBadge(p.confidence);
  if (badge) name.appendChild(badge);
  card.appendChild(name);

  const sub = [p.title, p.team].filter(Boolean).join(" · ");
  if (sub) card.appendChild(el("div", "sub", sub));
  chips(card, p.reasons);

  const details = personDetails(p);
  details.hidden = !expand;
  card.appendChild(details);
  toggle.addEventListener("click", () => {
    details.hidden = !details.hidden;
  });

  if (query && p.id) card.appendChild(voteButtons(query, { person: p.id }));
  return card;
}

function personDetails(p) {
  const wrap = el("div", "details");
  const row = (label, value) => {
    const r = el("div", "detail-row");
    r.appendChild(el("span", "detail-label", label));
    r.appendChild(value);
    wrap.appendChild(r);
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
  if (p.topics && p.topics.length) {
    const v = el("span", "detail-value detail-chips");
    for (const topic of p.topics) v.appendChild(el("span", "chip", topic));
    row("Knows about", v);
  }
  return wrap;
}

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
