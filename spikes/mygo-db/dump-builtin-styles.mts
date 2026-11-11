// 把 TS 内置配图风格导出为 Go 外壳嵌入用的 JSON（保持字段顺序）：
//   pnpm dlx tsx spikes/mygo-db/dump-builtin-styles.mts spikes/mygo-shell/illustration_builtin.json
// 对拍测试会再比一次 golden.configTransfer.builtinStyles，TS 侧内置预设变了测试会提示重新生成。
import fs from "node:fs";
import { BUILT_IN_ILLUSTRATION_STYLES } from "../../packages/core/src/illustration-styles.ts";

fs.writeFileSync(process.argv[2], `${JSON.stringify(BUILT_IN_ILLUSTRATION_STYLES, null, 2)}\n`, "utf8");