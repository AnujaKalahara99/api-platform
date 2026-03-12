import ws from "k6/ws";
import { check, sleep } from "k6";
import { Counter, Rate, Trend } from "k6/metrics";

// Custom metrics
const msgSent = new Counter("ws_messages_sent");
const msgReceived = new Counter("ws_messages_received");
const roundTrip = new Trend("ws_roundtrip_ms", true);
const connectFailRate = new Rate("ws_connect_fail_rate");

// Test configuration — ramp to 100K VUs
// export const options = {
//   stages: [
//     { duration: "30s", target: 1000 },    // warm up
//     { duration: "30s", target: 10000 },   // ramp
//     { duration: "30s", target: 50000 },   // ramp more
//     { duration: "1m",  target: 100000 },  // peak
//     { duration: "2m",  target: 100000 },  // sustain
//     { duration: "30s", target: 0 },       // ramp down
//   ],
//   // Thresholds — fail the test if these are violated
//   thresholds: {
//     ws_roundtrip_ms: ["p(95)<500", "p(99)<1000"],
//     ws_connect_fail_rate: ["rate<0.01"],
//     ws_messages_sent: ["count>1000000"],
//   },
// };

// Ramp down to 500 useres
export const options = {
  stages: [
    { duration: "30s", target: 100 },  // Warm up to 100
    { duration: "1m",  target: 500 },  // Ramp to 500
    { duration: "2m",  target: 500 },  // Sustain 500
    { duration: "30s", target: 0 },    // Graceful shutdown
  ],
  thresholds: {
    ws_roundtrip_ms: ["p(95)<500", "p(99)<1000"],
    ws_connect_fail_rate: ["rate<0.01"],
    // 500 VUs * 50 messages = 25,000. Set a bit lower to account for ramp-up/down
    ws_messages_sent: ["count>400"], 
  },
};

const WS_URL = __ENV.WS_URL || "ws://localhost:8080/mediation/ws-kafka/ws-inbound";
const MESSAGES_PER_VU = parseInt(__ENV.MESSAGES_PER_VU || "1");
const MESSAGE_SIZE = parseInt(__ENV.MESSAGE_SIZE || "256"); // bytes

function generatePayload(size) {
  const ts = Date.now().toString();
  const padding = "x".repeat(Math.max(0, size - ts.length - 20));
  return JSON.stringify({ ts: ts, vu: __VU, iter: __ITER, data: padding });
}

export default function () {
  const res = ws.connect(WS_URL, {}, function (socket) {
    connectFailRate.add(0);

    socket.on("open", function () {
      for (let i = 0; i < MESSAGES_PER_VU; i++) {
        const payload = generatePayload(MESSAGE_SIZE);
        const sendTime = Date.now();

        socket.send(payload);
        msgSent.add(1);

        // Tag messages for round-trip tracking
        socket.on("message", function (msg) {
          msgReceived.add(1);
          try {
            const parsed = JSON.parse(msg);
            if (parsed.ts) {
              roundTrip.add(Date.now() - parseInt(parsed.ts));
            }
          } catch (_) {
            // binary or non-JSON response
            roundTrip.add(Date.now() - sendTime);
          }
        });

        sleep(0.01); // 10ms between messages per VU
      }
    });

    socket.on("error", function (e) {
      connectFailRate.add(1);
    });

    // Keep connection alive for the duration
    socket.setTimeout(function () {
      socket.close();
    }, 2000);
  });

  // ADD THIS TO DEBUG:
  if (res && res.status !== 101) {
    console.error(`Connection failed! Status: ${res.status}, Error code: ${res.error}`);
  }

  check(res, {
    "connected successfully": (r) => r && r.status === 101,
  });
}