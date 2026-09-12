import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { execFileSync } from "node:child_process";
import test from "node:test";
import {
  createDesktopSnapshot,
  isValidationSnapshot,
} from "../desktop-validation-snapshot.mjs";
import { validationEnv } from "../isolated-desktop.mjs";

function fixture(t) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "guizhi-snapshot-test-"));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const write = (relative, text) => {
    const target = path.join(root, relative);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, text);
  };
  execFileSync("git", ["init", "--quiet"], { cwd: root });
  write(".gitignore", "node_modules/\nout/\n.env*\n");
  write("package.json", "{}");
  write("apps/desktop/src/main/index.ts", "tracked\r\n");
  write("apps/desktop/src/deleted.ts", "deleted");
  write("packages/core/package.json", '{"name":"@guizhi/core"}');
  execFileSync("git", ["add", "."], { cwd: root, stdio: "ignore" });
  write("apps/desktop/src/main/index.ts", "dirty\r\n");
  write("apps/desktop/src/new.ts", "untracked\r\n");
  write("apps/desktop/out/main/index.js", "dev-output");
  write("apps/desktop/.env.local", "secret");
  fs.unlinkSync(path.join(root, "apps/desktop/src/deleted.ts"));
  return { root, write };
}

test("副本包含当前未提交源码，忽略删除、产物、环境文件，写入不会回流", (t) => {
  const { root } = fixture(t);
  const before = execFileSync("git", ["status", "--porcelain"], {
    cwd: root,
  }).toString();
  const snapshot = createDesktopSnapshot(root);
  try {
    assert.equal(isValidationSnapshot(snapshot.desktopRoot), true);
    assert.equal(isValidationSnapshot(path.join(root, "apps/desktop")), false);
    assert.equal(
      fs.readFileSync(
        path.join(snapshot.desktopRoot, "src/main/index.ts"),
        "utf8",
      ),
      "dirty\r\n",
    );
    assert.equal(
      fs.readFileSync(path.join(snapshot.desktopRoot, "src/new.ts"), "utf8"),
      "untracked\r\n",
    );
    for (const relative of [
      "src/deleted.ts",
      "out/main/index.js",
      ".env.local",
    ]) {
      assert.equal(
        fs.existsSync(path.join(snapshot.desktopRoot, relative)),
        false,
      );
    }
    fs.writeFileSync(
      path.join(snapshot.desktopRoot, "src/main/index.ts"),
      "changed in copy",
    );
    assert.equal(
      fs.readFileSync(
        path.join(root, "apps/desktop/src/main/index.ts"),
        "utf8",
      ),
      "dirty\r\n",
    );
    assert.equal(
      execFileSync("git", ["status", "--porcelain"], { cwd: root }).toString(),
      before,
    );
  } finally {
    snapshot.cleanup();
  }
  assert.equal(fs.existsSync(snapshot.root), false);
  assert.equal(
    fs.readFileSync(path.join(root, "apps/desktop/out/main/index.js"), "utf8"),
    "dev-output",
  );
});

test("依赖包复用，Vite 缓存独立，工作区包指向副本，清理不删除原依赖", (t) => {
  const { root, write } = fixture(t);
  write("apps/desktop/node_modules/example/index.js", "dependency");
  write("apps/desktop/node_modules/.vite/cache", "dev-cache");
  fs.mkdirSync(path.join(root, "apps/desktop/node_modules/@guizhi"));
  fs.symlinkSync(
    path.join(root, "packages/core"),
    path.join(root, "apps/desktop/node_modules/@guizhi/core"),
    process.platform === "win32" ? "junction" : "dir",
  );
  const snapshot = createDesktopSnapshot(root);
  try {
    const modules = path.join(snapshot.desktopRoot, "node_modules");
    assert.equal(fs.lstatSync(modules).isSymbolicLink(), false);
    assert.equal(
      fs.readFileSync(path.join(modules, "example/index.js"), "utf8"),
      "dependency",
    );
    assert.equal(fs.existsSync(path.join(modules, ".vite/cache")), false);
    assert.equal(
      fs.realpathSync(path.join(modules, "@guizhi/core")),
      fs.realpathSync(path.join(snapshot.root, "packages/core")),
    );
    fs.mkdirSync(path.join(modules, ".vite"));
    fs.writeFileSync(path.join(modules, ".vite/cache"), "test-cache");
  } finally {
    snapshot.cleanup();
  }
  assert.equal(
    fs.readFileSync(
      path.join(root, "apps/desktop/node_modules/example/index.js"),
      "utf8",
    ),
    "dependency",
  );
  assert.equal(
    fs.readFileSync(
      path.join(root, "apps/desktop/node_modules/.vite/cache"),
      "utf8",
    ),
    "dev-cache",
  );
});

test("两个验证实例的构建目录互不覆盖", (t) => {
  const { root } = fixture(t);
  const first = createDesktopSnapshot(root);
  const second = createDesktopSnapshot(root);
  try {
    assert.notEqual(first.root, second.root);
    fs.mkdirSync(path.join(first.desktopRoot, "out"));
    fs.writeFileSync(path.join(first.desktopRoot, "out/marker"), "first");
    assert.equal(
      fs.existsSync(path.join(second.desktopRoot, "out/marker")),
      false,
    );
  } finally {
    first.cleanup();
    second.cleanup();
  }
});

test("验证子进程不继承用户开发服务地址和用户数据目录", () => {
  const env = {
    VITE_DEV_SERVER_URL: "http://127.0.0.1:5173",
    ELECTRON_RUN_AS_NODE: "1",
    GUIZHI_E2E_USER_DATA_DIR: "user-data",
    GUIZHI_E2E_RENDERER_URL: "dev-url",
    PATH: "tools",
  };
  const isolated = validationEnv(env);
  assert.equal(isolated.PATH, "tools");
  assert.equal(isolated.GUIZHI_WINDOW_MODE, "offscreen");
  for (const key of Object.keys(env).filter((key) => key !== "PATH"))
    assert.equal(isolated[key], undefined);
  assert.equal(env.GUIZHI_E2E_USER_DATA_DIR, "user-data");
});
