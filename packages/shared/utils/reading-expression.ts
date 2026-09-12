/** 有界递归下降算术解析器；也被编入离线页面，不执行 JavaScript 表达式。 */
export function evaluateReadingExpression(expression: string, values: Record<string, number>): number {
  if (typeof expression !== "string" || expression.length > 500) throw new Error("公式过长");
  const tokens = expression.match(/(?:\d+(?:\.\d+)?|\.[0-9]+)|[a-zA-Z_][a-zA-Z_0-9]*|[()+*/,\-]/g) ?? [];
  if (tokens.join("") !== expression.replace(/\s/g, "") || tokens.length > 200) throw new Error("公式含不支持的字符");
  let index = 0, depth = 0;
  const check = (n: number) => { if (!Number.isFinite(n) || Math.abs(n) > 1e15) throw new Error("计算结果超出范围"); return n; };
  function primary(): number {
    if (++depth > 30) throw new Error("公式嵌套过深");
    const token = tokens[index++]; let result: number;
    if (token === "+" || token === "-") result = (token === "-" ? -1 : 1) * primary();
    else if (token === "(") { result = sum(); if (tokens[index++] !== ")") throw new Error("括号不匹配"); }
    else if (token === "min" || token === "max") {
      if (tokens[index++] !== "(") throw new Error("函数缺少括号");
      const args = [sum()]; while (tokens[index] === ",") { index++; args.push(sum()); }
      if (tokens[index++] !== ")" || args.length < 2 || args.length > 10) throw new Error("函数参数无效");
      result = token === "min" ? Math.min(...args) : Math.max(...args);
    } else if (token && /^(?:\d|\.)/.test(token)) result = Number(token);
    else if (token && Object.prototype.hasOwnProperty.call(values, token)) result = values[token];
    else throw new Error("公式含未知变量");
    depth--; return check(result);
  }
  function product(): number {
    let result = primary();
    while (tokens[index] === "*" || tokens[index] === "/") {
      const op = tokens[index++], right = primary();
      if (op === "/" && right === 0) throw new Error("除数不能为零");
      result = check(op === "*" ? result * right : result / right);
    }
    return result;
  }
  function sum(): number {
    let result = product();
    while (tokens[index] === "+" || tokens[index] === "-") { const op = tokens[index++], right = product(); result = check(op === "+" ? result + right : result - right); }
    return result;
  }
  const result = sum(); if (index !== tokens.length) throw new Error("公式结构无效"); return result;
}
