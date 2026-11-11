// 把 TS 里内嵌的 FunASR 服务脚本按「求值后的字符串」原样导出为 funasr_server.py，供 Go 侧 go:embed。
import fs from "node:fs";
import { FUNASR_SERVER_SCRIPT } from "../../apps/desktop/src/main/services/media/funasr-server-script.ts";
fs.writeFileSync(process.argv[2], FUNASR_SERVER_SCRIPT, "utf8");
