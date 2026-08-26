// 前端操作台：展示后端实时状态，并调用锁定接口演示确定性质量闭环。
const backendStatus = document.getElementById("backend-status");
const backendVersion = document.getElementById("backend-version");
const lockResult = document.getElementById("lock-result");

function setStatus(ok, text) {
  backendStatus.textContent = text;
  backendStatus.className = ok ? "pill ok" : "pill err";
}

async function refreshHealth() {
  try {
    const res = await fetch("/api/v1/health");
    if (!res.ok) throw new Error("HTTP " + res.status);
    const data = await res.json();
    setStatus(true, "在线 · " + data.status);
    backendVersion.textContent = "";
  } catch (err) {
    setStatus(false, "离线 · " + err.message);
    backendVersion.textContent = "";
  }
}

function sampleBody(op, summaryVersion, invertible) {
  const t = invertible
    ? { a: 1, b: 0, c: 0, d: 0, e: 1, f: 0, scale: 1 }
    : { a: 1, b: 1, c: 0, d: 1, e: 1, f: 0, scale: 1 };
  return JSON.stringify({
    operation_id: op,
    building: "医院A楼",
    unit: "隔震单元U1",
    summary_version: summaryVersion,
    transform: t,
    positions: [
      {
        building: "医院A楼",
        unit: "隔震单元U1",
        axis_grid: "1-A",
        position_id: "P1",
        design_center: { x: 0, y: 0, z: 0 },
        orientation: { x: 0, y: 0, z: 1, scale: 1 },
        bearing_model: "LRB-500",
        upper: { id: "u1", orientation: { x: 0, y: 0, z: 1, scale: 1 }, plate_width: 600000, plate_length: 600000, hole_count: 4, hole_pattern: "square-200" },
        lower: { id: "l1", orientation: { x: 0, y: 0, z: -1, scale: 1 }, plate_width: 600000, plate_length: 600000, hole_count: 4, hole_pattern: "square-200" },
        allowed_eccentricity: 5000,
        allowed_tilt: 1000,
        tilt_scale: 3
      }
    ]
  });
}

async function lockUnit(body) {
  const res = await fetch("/api/v1/isolation-units", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body
  });
  const text = await res.text();
  let pretty;
  try {
    pretty = JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    pretty = text;
  }
  lockResult.textContent = `HTTP ${res.status}\n` + pretty;
}

document.getElementById("btn-lock").addEventListener("click", () => {
  lockUnit(sampleBody("op-" + Date.now(), "v1", true));
});
document.getElementById("btn-lock-bad").addEventListener("click", () => {
  lockUnit(sampleBody("op-" + Date.now(), "v1", false));
});

refreshHealth();
setInterval(refreshHealth, 5000);
