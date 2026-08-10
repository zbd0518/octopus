# Octopus Wiki

Welcome to the Octopus Wiki — a structured, navigable user documentation for the Octopus LLM API aggregation & load balancing service.

> Looking for the full single-file README? See [README.md](../README.md) (English) or [README_zh.md](../README_zh.md) (简体中文).

## How to browse

| Language | Index |
|----------|-------|
| English | [Home.md](Home.md) |
| 简体中文 | [Home_zh.md](Home_zh.md) |

Each page is self-contained and cross-linked with "previous / next / home" navigation at the bottom.

## Directory structure

```
wiki/
├── Home.md              # English index
├── Home_zh.md           # Chinese index
├── README.md            # This file
├── en/                  # English pages (01-13)
│   ├── 01-Installation.md
│   ├── 02-Configuration.md
│   ├── 03-Admin-Roles.md
│   ├── 04-Channels.md
│   ├── 05-Groups.md
│   ├── 06-Model-Market.md
│   ├── 07-Relay-Endpoints.md
│   ├── 08-Analytics.md
│   ├── 09-Ops.md
│   ├── 10-Settings.md
│   ├── 11-Hub-Sites.md
│   ├── 12-Client-Integration.md
│   └── 13-Architecture.md
└── zh/                  # Chinese pages (01-13)
    ├── 01-安装.md
    ├── 02-配置.md
    ├── 03-角色与管理员.md
    ├── 04-渠道管理.md
    ├── 05-分组管理.md
    ├── 06-模型广场.md
    ├── 07-Relay端点.md
    ├── 08-分析.md
    ├── 09-运维.md
    ├── 10-设置.md
    ├── 11-站点管理.md
    ├── 12-客户端接入.md
    └── 13-架构.md
```

## Contributing

Wiki pages are regular Markdown files tracked in Git. To add or edit a page:

1. Both `en/` and `zh/` versions must stay in sync — if you add an English page, add the corresponding Chinese page.
2. Keep page numbers sequential and update the index in both Home pages.
3. Preserve the "← Previous | Next → | Home" navigation block at the bottom of each page.

Image and screenshot references use relative paths back to the repo root (e.g. `../web/public/screenshot/...`), so the same images serve both languages.
