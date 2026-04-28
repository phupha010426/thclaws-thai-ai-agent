# ThaiAiAgent Handoff for Claude Code

วันที่: 2026-04-27
โปรเจกต์: `D:\code\thai-ai-agent`
Production: `https://thaiaiagent.smlsoft.app`
Droplet: `143.198.195.140`
Server path: `/opt/thaiaiagent`

## กติกาสำคัญจาก Jead

- คุยกับ Jead เป็นภาษาไทย สั้น ตรงประเด็น
- ห้ามใช้/อ่าน `D:\bcdev\clone-skills` สำหรับงานนี้ เพราะ Jead สั่งไว้ว่า "ไม่เอา clone-skills"
- LINE OA ห้าม push, multicast, broadcast เด็ดขาด ใช้ replyToken เท่านั้น
- ห้าม hardcode token/API key ทุกอย่างต้องอยู่ใน `.env`
- ห้ามเก็บรูปเป็น base64 ถาวร ใช้ base64 ได้เฉพาะใน request ชั่วคราวสำหรับ vision เท่านั้น
- รูปต้องเก็บใน MinIO
- MongoDB เป็น source of truth/log หลัก
- PostgreSQL เป็นตัวช่วยคำนวณบัญชีและ mirror เพื่อความเร็ว
- ข้อมูลต้องแยกตาม LINE userId/namespace ห้าม cross-user memory leak
- ห้าม AI แต่งเรื่อง/หลอน ถ้าไม่มีข้อมูลจริงให้บอกว่าไม่รู้หรือถามเพิ่ม
- thClaws ต้องเป็นหัวใจของระบบ เป็น planner/team lead ส่วน Node services เป็น executor
- SMLGateway ใช้ `sml/auto`, `sml/fast`, `sml/thai` ให้ gateway เลือก model เอง แต่ต้อง guard คุณภาพ

## โครงระบบ Production

Docker Compose services:

- `thaiaiagent-api`
- `thaiaiagent-worker`
- `thaiaiagent-caddy`
- `thaiaiagent-postgres`
- `thaiaiagent-mongodb`
- `thaiaiagent-redis`
- `thaiaiagent-minio`

Health:

- `https://thaiaiagent.smlsoft.app/health`
- ล่าสุด health เคยตอบ `{"ok":true,"service":"ThaiAiAgent"}`

วิธี SSH ที่ใช้ได้จากเครื่องนี้:

```powershell
docker run --rm -v "$env:USERPROFILE\.ssh:/hostssh:ro" alpine:3.20 sh -lc "apk add --no-cache openssh-client >/dev/null && cp /hostssh/id_ed25519 /tmp/id && chmod 600 /tmp/id && ssh -i /tmp/id -o StrictHostKeyChecking=no root@143.198.195.140 'echo ok'"
```

หมายเหตุ: Windows host `ssh/curl` มีอาการเพี้ยน เช่น `getaddrinfo() thread failed to start` หรือ ssh exit 255 เงียบ ๆ ให้ใช้ Docker Alpine เป็นตัวกลาง

## ไฟล์ที่แก้/เพิ่มในรอบล่าสุด

แก้:

- `src/modules/agents/image-understanding.service.ts`
- `src/modules/agents/sml-gateway.service.ts`
- `src/modules/agents/thclaws-agent.service.ts`
- `src/config/env.ts`
- `.env.example`

เพิ่ม:

- `.codex-chat-test.mjs`
- `.codex-set-prefer-and-rebuild.sh`
- `HANDOFF_FOR_CLAUDE_CODE.md`

## สิ่งที่ทำแล้ว

### 1. Vision/Image

แก้ `image-understanding.service.ts`:

- AI vision ส่งรูปเป็น data URL เฉพาะ request ชั่วคราว
- ไม่ persist base64
- เพิ่ม parser แบบ loose JSON
- ถ้า model ตอบ JSON เบี้ยว จะพยายาม sanitize/control char/ดึง field สำคัญ
- ถ้ายัง parse ไม่ได้ จะ fallback เป็นข้อความจาก AI แบบไม่แต่งข้อมูล และไม่ทำให้ image job ล้มทั้งงาน

อาการก่อนแก้:

- รูปบางใบถูกเก็บใน MinIO แล้ว แต่ AI ตอบ malformed JSON ทำให้ log ขึ้น `line image handling failed`

### 2. SMLGateway

แก้ `sml-gateway.service.ts`:

- รองรับ headers:
  - `X-SMLGateway-Strategy`
  - `X-SMLGateway-Max-Latency`
  - `X-SMLGateway-Prefer`
  - `X-SMLGateway-Exclude`
- `/structured` ของ gateway ตอนนี้ production มักตอบ 422 พร้อม `HTTP 401 from gateway`
- เปลี่ยน log ของเคสนี้เป็น info: `SMLGateway structured unavailable, falling back to chat JSON`
- fallback ไป chat JSON ยังทำงาน

### 3. thClaws Planner

แก้ `thclaws-agent.service.ts`:

- เพิ่ม deterministic planner ก่อนเข้า AI สำหรับงานบัญชีง่าย:
  - `กาแฟ 60`
  - `สรุปวันนี้`
  - `สรุปเดือนนี้`
  - `ช่วยเหลือ`
- ผลคือบัญชี/summary ไม่ต้องรอ AI และไม่โดน model หลอน
- action ที่ไม่ครบ เช่น `remember_memory` ไม่มี `content` จะถูกทิ้งและ repair เป็น `no_op` แทนทำให้ทั้งระบบตอบว่า thClaws ไม่พร้อม
- เพิ่ม direct memory command:
  - `จำไว้ว่า...`
  - `จำว่า...`
  - `ช่วยจำว่า...`
- เพิ่ม context question แบบง่าย:
  - ถามว่า `เมื่อกี้ผมบอกว่าไม่กินอะไร` จะพยายามตอบจาก context/memory
- เพิ่ม general chat path ใช้ `GENERAL_AGENT_MODEL=sml/thai`

## Env ที่เพิ่ม

เพิ่มใน `src/config/env.ts` และ `.env.example`:

```env
GENERAL_AGENT_MODEL=sml/thai
SMLGATEWAY_PREFER_PROVIDERS=typhoon,thaillm,groq,cerebras
SMLGATEWAY_TEXT_EXCLUDE_PROVIDERS=mistral
```

เหตุผล:

- `mistral/codestral` ตอบไทยและสูตรอาหารเพี้ยนหลายครั้ง
- `sml/auto` ยังเลือก provider แปลกบางรอบ จึงต้อง prefer/exclude เฉพาะ text planner
- Vision ยังไม่ได้ exclude mistral เพราะเคยอ่านรูปได้

## ผลทดสอบ Build/Test

บน local Docker Node:

```powershell
docker run --rm -v "${PWD}:/app" -w /app node:22-bookworm npm run build
docker run --rm -v "${PWD}:/app" -w /app node:22-bookworm npm test
```

ล่าสุดก่อน deploy ผ่าน:

- build ผ่าน
- test ผ่าน 14/14
- test สำคัญ:
  - `LINE OA must not push, multicast, or broadcast messages`
  - `image base64 must not be persisted in schemas or storage models`
  - parser 10+ cases

## Deploy ล่าสุด

Deploy ล่าสุดสำเร็จหลังแก้ path `src/config/env.ts`:

```sh
cd /opt/thaiaiagent
docker compose -f docker-compose.prod.yml up -d --build api worker
```

api/worker start สำเร็จหลัง build

คำเตือน: หลังเพิ่ม `GENERAL_AGENT_MODEL=sml/thai` และ direct general chat ยังไม่ได้รัน full random test ซ้ำจนจบ เพราะ Jead สั่งให้ save handoff ก่อน

## Log/พฤติกรรมที่พบจากการสุ่มคุย

ก่อนแก้ deterministic planner:

- `กาแฟ 60` ผ่าน แต่รอ AI 0.9-7 วินาที แล้วแต่ provider
- `วันนี้จ่ายค่าอะไรไปบ้าง` ผ่านเป็น action `get_today_summary`
- `เดือนนี้ใช้ไปเท่าไร` ผ่านเป็น action `get_month_summary`
- `จำไว้ว่าผมไม่กินเผ็ด` บางครั้งได้ `remember_memory`, บางครั้ง `update_wiki`
- `เมื่อกี้ผมบอกว่าไม่กินอะไร` ยังมีรอบที่ตอบผิดว่า "ไม่ทราบว่าคุณกินอะไรเมื่อกี้"
- `ขอสูตรส้มตำหน่อย` เคยเพี้ยนหนักเมื่อ gateway route ไป `mistral/codestral`
- `sml/auto` เคย route ไป:
  - `mistral/codestral-2508`
  - `typhoon-v2.5-30b-a3b-instruct`
  - `thaillm/Pathumma...`
  - `openrouter/openai/gpt-oss-20b:free`
  - `nvidia/llama-3.1-nemotron-nano-vl-8b-v1`
- Provider บางตัวช้า 18-29 วินาที
- Groq fallback เคยเจอ 429 quota เต็ม

หลังแก้ deterministic planner:

- `กาแฟ 60` เป็น `thClaws deterministic planner success` latency ประมาณ 6 ms
- `วันนี้จ่ายค่าอะไรไปบ้าง` เป็น deterministic latency ประมาณ 4 ms
- `เดือนนี้ใช้ไปเท่าไร` เป็น deterministic latency ประมาณ 9 ms
- general chat ยังควรทดสอบซ้ำหลัง deploy ล่าสุด

## จุดที่ยังควรแก้ต่อ

1. General chat quality

- ทดสอบ `GENERAL_AGENT_MODEL=sml/thai` หลัง deploy ล่าสุด
- ถ้ายัง route แปลก ให้พิจารณาใช้ model ตรงเช่น `typhoon/typhoon-v2.5-30b-a3b-instruct` สำหรับ general Thai chat
- แต่อย่าทิ้ง `sml/auto` ทั้งหมด เพราะ Jead ต้องการให้ gateway เลือกเอง

2. History/memory ต่อเนื่อง

- มี collections แล้ว:
  - `conversation_messages`
  - `memory_summaries`
  - `event_logs`
  - `memory_items`
  - `personal_wiki_pages`
- แต่การถามอ้างอิงยังควรทำ retrieval ให้แน่นกว่านี้
- ควรเพิ่ม service สำหรับ answer-from-memory โดยอ่าน context จริงก่อนเข้า LLM

3. Summary/ledger truth

- ทดสอบผ่าน LINE จริงว่า `summaryService` ดึงจาก DB จริง ไม่ใช่ finalReply ของ AI
- deterministic planner ส่ง action เท่านั้น ใน LINE flow `executePlan()` จะเรียก summaryService จริง
- `.codex-chat-test.mjs` เป็น planner test ไม่ได้ execute full LINE plan ทุก action

4. SMLGateway structured

- `/structured` production ยัง 422 `HTTP 401 from gateway`
- ตอนนี้ fallback chat JSON ช่วยไว้
- ถ้ามีสิทธิ์แก้ SMLGateway ควรตรวจ auth ภายใน endpoint `/v1/structured`

5. Image/slip

- ทดสอบส่ง slip LINE จริงอีกครั้งหลัง image parser fix
- ควรตรวจว่า stored image เข้า MinIO และ `slip_records` updated
- ห้าม auto-post slip เข้าบัญชีจนกว่าผู้ใช้ยืนยัน

6. Clean dev scripts

- `.codex-chat-test.mjs` และ `.codex-set-prefer-and-rebuild.sh` เป็น helper ชั่วคราว
- จะเก็บไว้ให้ Claude Code ใช้ debug ต่อได้
- ก่อน production cleanup อาจย้ายไป `scripts/ops/`

## คำสั่งที่มีประโยชน์

ดู container/log:

```powershell
docker run --rm -v "$env:USERPROFILE\.ssh:/hostssh:ro" alpine:3.20 sh -lc "apk add --no-cache openssh-client >/dev/null && cp /hostssh/id_ed25519 /tmp/id && chmod 600 /tmp/id && ssh -i /tmp/id -o StrictHostKeyChecking=no root@143.198.195.140 'cd /opt/thaiaiagent && docker compose -f docker-compose.prod.yml ps && docker compose -f docker-compose.prod.yml logs --tail=200 api worker'"
```

Deploy:

```powershell
docker run --rm -v "$env:USERPROFILE\.ssh:/hostssh:ro" -v "${PWD}:/work:ro" alpine:3.20 sh -lc "apk add --no-cache openssh-client >/dev/null && cp /hostssh/id_ed25519 /tmp/id && chmod 600 /tmp/id && scp -i /tmp/id -o StrictHostKeyChecking=no /work/src/config/env.ts root@143.198.195.140:/opt/thaiaiagent/src/config/env.ts && scp -i /tmp/id -o StrictHostKeyChecking=no /work/src/modules/agents/thclaws-agent.service.ts root@143.198.195.140:/opt/thaiaiagent/src/modules/agents/thclaws-agent.service.ts && ssh -i /tmp/id -o StrictHostKeyChecking=no root@143.198.195.140 'cd /opt/thaiaiagent && docker compose -f docker-compose.prod.yml up -d --build api worker'"
```

Run planner test on server:

```powershell
docker run --rm -v "$env:USERPROFILE\.ssh:/hostssh:ro" -v "${PWD}:/work:ro" alpine:3.20 sh -lc "apk add --no-cache openssh-client >/dev/null && cp /hostssh/id_ed25519 /tmp/id && chmod 600 /tmp/id && scp -i /tmp/id -o StrictHostKeyChecking=no /work/.codex-chat-test.mjs root@143.198.195.140:/opt/thaiaiagent/.codex-chat-test.mjs && ssh -i /tmp/id -o StrictHostKeyChecking=no root@143.198.195.140 'cd /opt/thaiaiagent && docker cp .codex-chat-test.mjs thaiaiagent-api:/app/.codex-chat-test.mjs && docker compose -f docker-compose.prod.yml exec -T api node .codex-chat-test.mjs'"
```

## ข้อควรระวัง

- อย่าสรุปว่า LINE ใช้งานจริงดีจาก `.codex-chat-test.mjs` อย่างเดียว เพราะ script นี้ทดสอบ planner ไม่ได้ใช้ `executePlan()` ครบเหมือน worker
- ถ้าจะทดสอบ LINE จริง ให้ดูว่าไม่มี push และมี replyToken เท่านั้น
- อย่า print secrets จาก `.env`
- อย่าให้ AI ตอบจาก memory ของ namespace อื่น
- อย่าเก็บ raw image/base64 ใน Mongo
