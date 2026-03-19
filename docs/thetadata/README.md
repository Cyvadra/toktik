# ThetaData 文档整理

本目录用于沉淀 ThetaData 接口的本地调用文档，方便开发与排障。

## 文件说明

- `options-rest-api.md`: ThetaData v3 期权 REST API 全量整理（List / Snapshot / History / At-Time），包含：
  - 接口地址
  - 输入参数
  - 输出字段
  - 接口描述
  - 最低权限级别（订阅）
  - 示例调用 URL

## 数据来源

- https://docs.thetadata.us/operations/option_list_symbols.html
- https://docs.thetadata.us/openapiv3.yaml

## 备注

- 默认 Base URL: `http://127.0.0.1:25503/v3`
- 使用前需确保本地 v3 Theta Terminal 正常运行。
