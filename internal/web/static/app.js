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
  const q = qInput.value.trim();
  if (!q) return;

  button.disabled = true;
  clearResults();
  statusEl.textContent = modeSel.value === "llm" ? "Asking the local model..." : "Searching...";

  try {
    const params = new URLSearchParams({ q, mode: modeSel.value });
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
});

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

  for (const p of people) peopleEl.appendChild(personCard(p));
  for (const c of channels) channelsEl.appendChild(channelCard(c));
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

function personCard(p) {
  const card = el("div", "card");
  card.appendChild(el("div", "name", p.name || p.email || "unknown"));
  const sub = [p.title, p.team].filter(Boolean).join(" · ");
  if (sub) card.appendChild(el("div", "sub", sub));
  if (p.email) card.appendChild(el("div", "sub", p.email));
  chips(card, p.reasons);
  return card;
}

function channelCard(c) {
  const card = el("div", "card");
  card.appendChild(el("div", "name", "#" + c.name));
  if (c.topic) card.appendChild(el("div", "sub", c.topic));
  const members = (c.members || []).map((m) => m.name).filter(Boolean);
  if (members.length) card.appendChild(el("div", "sub", "Active: " + members.join(", ")));
  chips(card, c.reasons);
  return card;
}
