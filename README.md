# ✨ Xiaozhi AI Chatbot Backend Service (Commercial Edition)

Xiaozhi AI is a voice interaction robot that combines powerful models like Qwen and DeepSeek, connecting multiple devices (ESP32, Android, Python, etc.) through the MCP protocol to achieve efficient and natural human-machine dialogue.

This project is its backend service, aiming to provide a **commercial-grade deployment solution** - high concurrency, low cost, complete functionality, and ready to use out of the box.

<p align="center">
  <img src="https://github.com/user-attachments/assets/aa1e2f26-92d3-4d16-a74a-68232f34cca3" alt="Xiaozhi Architecture" width="600">
</p>

The project was initially based on [Xiaoge's ESP32 open source project](https://github.com/78/xiaozhi-esp32?tab=readme-ov-file), and has now formed a complete ecosystem supporting multiple client protocol compatibility.

---

## ✨ Core Advantages

| Advantage | Description |
| ---------- | ---------------------------------------------------- |
| 🚀 High Concurrency | Single machine supports 3000+ online users, distributed scaling to millions of users |
| 👥 User System | Complete user registration, login, and permission management capabilities |
| 💰 Payment Integration | Integrated payment system to help commercial closed-loop |
| 🛠️ Flexible Model Integration | Support calling multiple large models through API, simplified deployment, supports custom local deployment |
| 📈 Commercial Support | Provides 7×24 technical support and operation guarantee |
| 🧠 Model Compatibility | Supports ASR (Doubao), TTS (EdgeTTS), LLM (OpenAI, Ollama), Image Description (Zhipu), etc. |

---

## ✅ Feature List

* [x] Support WebSocket connections
* [x] Support PCM / Opus format voice dialogue
* [x] Support large models: ASR (Doubao streaming), TTS (EdgeTTS/Doubao), LLM (OpenAI API, Ollama)
* [x] Support voice control to call camera for image recognition (Zhipu API)
* [x] Support auto/manual/realtime three dialogue modes, support real-time dialogue interruption
* [x] Support ESP32 Xiaozhi client, Python client, Android client connection without verification
* [x] OTA firmware distribution
* [x] Support MCP protocol (client / local / server), can integrate Amap, weather query, etc.
* [x] Support voice control to switch character voices
* [x] Support voice control to switch preset characters
* [x] Support voice control to play music
* [x] Support single machine deployment service
* [x] Support local database sqlite
* [x] Support Coze workflow
* [x] Support Docker deployment
* [x] Support MySQL, PostgreSQL (Commercial Edition feature)
* [x] Support MQTT connection (Commercial Edition feature)
* [x] Support Dify workflow (Commercial Edition feature)
* [x] Management backend (Commercial Edition completed: device binding, user, agent management)


---

## 🚀 Quick Start

### 1. Download Release Version

> Recommended to directly download the Release version without configuring the development environment:

👉 [Click to go to Releases page](https://github.com/AnimeAIChat/xiaozhi-server-go/releases)

* Choose the version corresponding to your platform (e.g., Windows: `windows-amd64-server.exe`)
* `.upx.exe` is a compressed version with the same functionality but smaller size, suitable for remote deployment

---

### 2. Configure `.config.yaml`

* Recommended to copy `config.yaml` and rename it to `.config.yaml`
* Configure model, WebSocket, OTA address and other fields as needed
* It is not recommended to add or remove field structures on your own

#### WebSocket Address Configuration (Required)

```yaml
web:
  websocket: ws://your-server-ip:8000
```

Used for OTA service to distribute connection addresses to clients. ESP32 clients will automatically connect to WS from this address without manual configuration.

Note: If it's LAN debugging, your-server-ip should be configured as **the IP of your computer in the LAN**, and the terminal device and computer should be on the same network segment for the device to connect to the service on your computer through this IP address.

#### OTA Address Configuration (Required)

```text
http://your-server-ip:8080/api/ota/
```

> ESP32 firmware has built-in OTA address. Ensure this service address is available. **After the service is running, you can output this address in the browser to confirm the service is accessible**.

ESP32 devices can modify the OTA address in the network interface, thus switching backend services without reflashing the firmware.

#### Configure ASR, LLM, TTS

Configure related model services according to the configuration file format, try not to add or remove fields

---

## 💬 MCP 协议配置

Reference: `src/core/mcp/README.md`

---

## 🧪 Source Code Installation and Running

### Prerequisites

* Go 1.24.2+
* Windows users need to install CGO and Opus libraries (see below)

```bash
git clone https://github.com/AnimeAIChat/xiaozhi-server-go.git
cd xiaozhi-server-go
cp config.yaml .config.yaml
```

---

### Windows Opus Compilation Environment Installation

Install [MSYS2](https://www.msys2.org/), open MYSY2 MINGW64 console, then enter the following commands:

```bash
pacman -Syu
pacman -S mingw-w64-x86_64-gcc mingw-w64-x86_64-go mingw-w64-x86_64-opus
pacman -S mingw-w64-x86_64-pkg-config
```

Set environment variables (for PowerShell or system variables):

```bash
set PKG_CONFIG_PATH=C:\msys64\mingw64\lib\pkgconfig
set CGO_ENABLED=1
```

尽量在MINGW64环境下运行一次 “go run ./src/main.go” 命令，确保服务正常运行

GO mod如果更新较慢，可以考虑设置go代理，切换国内镜像源。

---

### Run Project

```bash
go mod tidy
go run ./src/main.go
```

### Compile Release Version

```bash
go build -o xiaozhi-server.exe src/main.go
```

### Testing
* Recommended to use ESP32 hardware device for testing to avoid compatibility issues to the greatest extent
* Recommended to use Xuanfeng Xiaozhi Android client, add the local service's OTA address in the settings interface. Android version is released on the Release page, you can choose the latest version
  <img width="221" height="470" alt="image" src="https://github.com/user-attachments/assets/145a6612-8397-439b-9429-325855a99101" />

  [xiaozhi-0.0.6.apk](https://github.com/AnimeAIChat/xiaozhi-server-go/releases/download/v0.1.0/xiaozhi-0.0.6.apk)
* Can use other clients compatible with Xiaozhi protocol for testing
---

## 📚 Swagger Documentation

* Open browser and visit: `http://localhost:8080/swagger/index.html`

### Update Swagger Documentation (run after every API modification)

```bash
cd src
swag init -g main.go
```

---

## ☁️ CentOS Source Code Deployment Guide

> Documentation: [Centos 8 Installation Guide](Centos_Guide.md)

---

## Docker Environment Deployment

1. Prepare `docker-compose.yml`, `.config.yaml`, binary program files

👉 [Click to go to Releases page](https://github.com/AnimeAIChat/xiaozhi-server-go/releases) to download binary program files

* Choose the version corresponding to your platform (default uses Linux: `linux-amd64-server-upx`, if using other versions, need to modify docker-compose.yml)

2. Put the three files in the same directory, configure `docker-compose.yml`, `.config.yaml`

3. Run `docker compose up -d`

---

## 💬 Community Support

Welcome to submit Issues, PRs or new feature suggestions!

<img src="https://github.com/user-attachments/assets/58b2f34c-a6ec-494f-a231-5f5f71cf6343" width="450" alt="WeChat Group QR Code">

---

## 🛠️ Custom Development

We accept various customized development projects. If you have specific needs, please contact us via WeChat for discussion.

<img src="https://github.com/user-attachments/assets/e2639bc3-a58a-472f-9e72-b9363f9e79a3" width="450" alt="Group Owner QR Code">

## 📄 License

本仓库遵循 `Xiaozhi-server-go Open Source License`（基于 Apache 2.0 增强版）
