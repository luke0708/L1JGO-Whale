# 待推送變更

<!-- 每次修改程式碼時在此記錄，推送後清空 -->

- feat: HP/MP 上限擴展至 int32（最大 9,999,999），DB migration 027，封包改 WriteD
- feat: 客戶端文字編碼可配置化（server.toml client_language_code: MS950/GBK），新增 packet/encoding.go
- feat: 裝備欄位系統擴展（equipment.go 支援更多部位）+ .speed GM 指令
- feat: 新增測試用防具資料（armor_list.yaml）
