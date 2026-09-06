import assert from "node:assert/strict";

// 仅在 pnpm shot 的隔离实例中替换 IPC，不连接真实收件箱或生成真实凭证。
export default async ({ win, app, shot }) => {
  await app.evaluate(({ ipcMain, BrowserWindow }) => {
    BrowserWindow.getAllWindows()[0].setSize(1600, 1100);
    globalThis.capturePreview = {
      settings: {
        connected: true,
        persistent: true,
        paused: false,
        origin: "https://capture.couleurapp.com",
        mailboxId: "preview",
        collectionId: null,
      },
      devices: [
        { id: "phone-1", name: "Mate80 Pro Max", kind: "phone", active: 1 },
      ],
      pairings: [],
      pairDuration: 300000,
      calls: [],
      failDevices: false,
    };
    ipcMain.removeHandler("mobile-capture:invoke");
    ipcMain.handle(
      "mobile-capture:invoke",
      async (_event, action, args = {}) => {
        const preview = globalThis.capturePreview;
        preview.calls.push({ action, args });
        let data;
        switch (action) {
          case "status":
            data = preview.settings;
            break;
          case "devices":
            if (preview.failDevices)
              return { success: false, error: "设备列表暂时无法读取，请重试" };
            data = preview.devices;
            break;
          case "pairings":
            data = preview.pairings;
            break;
          case "configure":
            Object.assign(preview.settings, args);
            data = preview.settings;
            break;
          case "fetch":
            preview.settings.lastReceivedAt = Date.now();
            data = preview.settings;
            break;
          case "pair":
            data = {
              id: "pair-1",
              expiresAt: Date.now() + preview.pairDuration,
              url: "https://example.invalid/#pair=preview-only",
            };
            break;
          case "confirm":
            if (preview.holdConfirm)
              await new Promise((resolve) => {
                globalThis.releaseCaptureConfirm = resolve;
              });
            if (preview.failConfirm)
              return { success: false, error: "确认暂时失败，请重试" };
            preview.devices.push({
              id: args.deviceId,
              name: "我的另一台手机",
              kind: "phone",
              active: 1,
            });
            preview.pairings = [];
            break;
          case "revoke":
            preview.devices = preview.devices.filter(
              (device) => device.id !== args.id,
            );
            break;
          case "disable":
            preview.settings.connected = false;
            preview.devices = [];
            break;
          case "activate":
            preview.settings.connected = true;
            data = preview.settings;
            break;
          default:
            return { success: false, error: `未实现的预览操作：${action}` };
        }
        return { success: true, data };
      },
    );
  });
  const appearance = async (theme, language = "zh") => {
    await win.evaluate(
      ({ theme, language }) => {
        const stored = JSON.parse(
          localStorage.getItem("guizhi-settings") || '{"state":{}}',
        );
        Object.assign(stored.state, {
          language,
          themeMode: theme,
          isDarkMode: theme === "dark",
        });
        localStorage.setItem("guizhi-settings", JSON.stringify(stored));
        localStorage.setItem("guizhi-setup-dismissed", "1");
        localStorage.setItem("guizhi-migration-dismissed", "1");
      },
      { theme, language },
    );
    await win.reload();
    await win.getByTestId("rail-settings").click();
    await win.getByTestId("settings-nav-mobile-capture").click();
  };
  const reloadSection = async () => {
    await win.getByTestId("settings-nav-network").click();
    await win.getByTestId("settings-nav-mobile-capture").click();
  };
  const page = win.getByTestId("mobile-capture-settings");
  const noOverflow = async () => {
    const overflow = await page.evaluate((element) =>
      [...element.querySelectorAll("section")].some(
        (section) => section.scrollWidth > section.clientWidth + 1,
      ),
    );
    assert.equal(overflow, false, "手机收集卡片不应横向溢出");
  };
  await appearance("dark");
  const collection = await win.evaluate(() =>
    window.api.collection.create({ name: "手机灵感" }),
  );
  await reloadSection();
  await page.getByText("Mate80 Pro Max", { exact: true }).waitFor();
  await noOverflow();
  await shot("mobile-capture-dark");
  await page.getByRole("button", { name: "暂停取件", exact: true }).click();
  await page.getByText("取件已暂停", { exact: true }).waitFor();
  assert.equal(
    await page
      .getByRole("button", { name: "立即取件", exact: true })
      .isDisabled(),
    true,
  );
  await page.getByRole("button", { name: "恢复取件", exact: true }).click();
  await page
    .getByRole("button", { name: "默认目标知识库", exact: true })
    .click();
  await win.getByRole("option", { name: "手机灵感", exact: true }).click();
  assert.equal(
    await app.evaluate(() => globalThis.capturePreview.settings.collectionId),
    collection.id,
  );
  await page.getByRole("button", { name: "立即取件", exact: true }).click();
  await page
    .getByText("尚未取回内容", { exact: true })
    .waitFor({ state: "detached" });
  await app.evaluate(() => {
    globalThis.capturePreview.pairDuration = 1500;
  });
  await page
    .getByRole("button", { name: "生成配对二维码", exact: true })
    .click();
  await page.getByRole("img", { name: "手机配对二维码" }).waitFor();
  await page.getByText("二维码已过期，请重新生成", { exact: true }).waitFor();
  assert.equal(
    await page.getByRole("img", { name: "手机配对二维码" }).count(),
    0,
  );
  await app.evaluate(() => {
    globalThis.capturePreview.pairDuration = 300000;
  });
  await page
    .getByRole("button", { name: "重新生成二维码", exact: true })
    .click();
  await page.getByText("等待手机提交绑定请求…", { exact: true }).waitFor();
  await shot("mobile-capture-waiting");
  await app.evaluate(() => {
    globalThis.capturePreview.pairings = [
      {
        id: "pair-1",
        deviceId: "phone-2",
        name: "我的另一台手机",
        expiresAt: Date.now() + 300000,
      },
    ];
  });
  await page
    .getByText("有设备正在等待你的确认", { exact: true })
    .waitFor({ timeout: 4500 });
  await shot("mobile-capture-pairing");
  await app.evaluate(() => {
    globalThis.capturePreview.holdConfirm = true;
    globalThis.capturePreview.failConfirm = true;
  });
  await page
    .getByRole("button", { name: "确认绑定此设备", exact: true })
    .click();
  await page
    .getByRole("button", { name: "正在确认绑定…", exact: true })
    .waitFor();
  await shot("mobile-capture-confirming");
  await app.evaluate(() => {
    globalThis.releaseCaptureConfirm();
  });
  await page.getByText("确认暂时失败，请重试", { exact: true }).waitFor();
  await app.evaluate(() => {
    globalThis.capturePreview.holdConfirm = false;
    globalThis.capturePreview.failConfirm = false;
  });
  await page
    .getByRole("button", { name: "确认绑定此设备", exact: true })
    .click();
  await page
    .getByText("绑定成功，手机页面会自动更新，现在可以开始收集。", {
      exact: true,
    })
    .waitFor();
  await shot("mobile-capture-confirmed");
  await page
    .getByRole("button", { name: "解除绑定 我的另一台手机", exact: true })
    .waitFor();
  await page
    .getByRole("button", { name: "解除绑定 我的另一台手机", exact: true })
    .click();
  await win.getByRole("button", { name: "取消", exact: true }).click();
  assert.equal(
    await app.evaluate(
      () =>
        globalThis.capturePreview.calls.filter(
          (call) => call.action === "revoke",
        ).length,
    ),
    0,
  );
  await page
    .getByRole("button", { name: "解除绑定 我的另一台手机", exact: true })
    .click();
  await win
    .getByRole("alertdialog")
    .getByRole("button", { name: "解除绑定", exact: true })
    .click();
  await page
    .getByRole("button", { name: "解除绑定 我的另一台手机", exact: true })
    .waitFor({ state: "detached" });
  await appearance("light");
  await page.getByText("Mate80 Pro Max", { exact: true }).waitFor();
  await shot("mobile-capture-light");
  await app.evaluate(({ BrowserWindow }) =>
    BrowserWindow.getAllWindows()[0].setSize(1000, 900),
  );
  await noOverflow();
  await shot("mobile-capture-narrow");
  await appearance("light", "en");
  await page.getByText("Mate80 Pro Max", { exact: true }).waitFor();
  await noOverflow();
  await shot("mobile-capture-english");
  await appearance("dark");
  await app.evaluate(() => {
    globalThis.capturePreview.failDevices = true;
  });
  await reloadSection();
  await page
    .getByText("设备列表暂时无法读取，请重试", { exact: true })
    .waitFor();
  assert.equal(
    await page.getByText("还没有绑定设备", { exact: true }).count(),
    0,
  );
  await app.evaluate(() => {
    globalThis.capturePreview.failDevices = false;
    globalThis.capturePreview.devices = [];
  });
  await page.getByRole("button", { name: "重试", exact: true }).click();
  await page.getByText("还没有绑定设备", { exact: true }).waitFor();
  await page.getByRole("button", { name: "停用此收件箱", exact: true }).click();
  await win
    .getByRole("alertdialog")
    .getByRole("button", { name: "确认停用", exact: true })
    .click();
  await page.getByText("连接手机收件箱", { exact: true }).waitFor();
  await shot("mobile-capture-activation");
  console.log(
    "手机收集离屏验证通过：深浅主题、中英文、窄屏、暂停恢复、取件、二维码过期、确认配对、取消及解除绑定、加载失败重试、空设备与停用状态。所有收件箱操作使用隔离 IPC 样例。",
  );
};
