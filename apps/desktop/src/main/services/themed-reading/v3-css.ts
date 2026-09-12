import * as css from "css-tree";
import {
  readingSvgReference,
  type ReadingSvgReference,
} from "./reconstruction-svg";

/** v3 样式属于隔离页面；允许动画规则，资源仍须来自受控素材清单。 */
export function cleanV3Css(
  value: string,
  inline = false,
  references: ReadingSvgReference[] = [],
  staticOnly = false,
): string {
  if (typeof value !== "string" || value.length > 300000)
    throw new Error("页面样式过大");
  const tree = css.parse(value, {
    context: inline ? "declarationList" : "stylesheet",
    parseCustomProperty: true,
  });
  css.walk(tree, function (node, item, list) {
    if (node.type === "Url")
      references.push(
        readingSvgReference(this.declaration?.property ?? "", node.value),
      );
    if (node.type === "Raw") throw new Error("样式语法无效");
    if (
      node.type === "Atrule" &&
      ![
        "media",
        "supports",
        "keyframes",
        "-webkit-keyframes",
        "container",
        "layer",
        "starting-style",
      ].includes(node.name.toLowerCase())
    )
      throw new Error("样式不能加载外部资源或字体");
    if (
      node.type === "Function" &&
      ["url", "expression", "paint", "image", "image-set", "attr"].includes(
        node.name.toLowerCase(),
      )
    )
      throw new Error("样式资源函数不允许");
    if (node.type === "Declaration") {
      const property = node.property.toLowerCase(),
        value = css.generate(node.value).toLowerCase();
      if (["behavior", "-moz-binding"].includes(property))
        throw new Error("样式行为不允许");
      // 静态阅读展开交互面板，保留核心文字；动效和隐藏规则不进入静态导出。
      if (
        staticOnly &&
        list &&
        item &&
        ((property === "display" && value === "none") ||
          (property === "visibility" && value === "hidden") ||
          (property === "opacity" && /^0(?:\.0+)?$/.test(value)) ||
          property.startsWith("animation"))
      )
        list.remove(item);
    }
  });
  return css.generate(tree).replace(/<\//g, "<\\/");
}
