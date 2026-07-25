interface ParsedVersion {
  major: number;
  minor: number;
  patch: number;
  prerelease: Array<number | string>;
}

function parsePrereleaseIdentifier(value: string): number | string {
  return /^\d+$/.test(value) ? Number(value) : value;
}

export function normalizeVersion(value: string): string {
  return value.trim().replace(/^v/, "").split("+")[0] || "0.0.0";
}

export function parseVersion(value: string): ParsedVersion | null {
  const normalized = normalizeVersion(value);
  const match =
    /^(\d+)\.(\d+)\.(\d+)(?:-([0-9A-Za-z.-]+))?$/.exec(normalized);

  if (!match) {
    return null;
  }

  return {
    major: Number(match[1]),
    minor: Number(match[2]),
    patch: Number(match[3]),
    prerelease: match[4]
      ? match[4].split(".").map(parsePrereleaseIdentifier)
      : [],
  };
}

function comparePrereleaseIdentifiers(
  left: number | string,
  right: number | string,
): number {
  if (left === right) {
    return 0;
  }

  if (typeof left === "number" && typeof right === "number") {
    return left > right ? 1 : -1;
  }

  if (typeof left === "number") {
    return -1;
  }

  if (typeof right === "number") {
    return 1;
  }

  return left > right ? 1 : -1;
}

export function compareVersions(left: string, right: string): number {
  const parsedLeft = parseVersion(left);
  const parsedRight = parseVersion(right);

  if (!parsedLeft || !parsedRight) {
    const normalizedLeft = normalizeVersion(left);
    const normalizedRight = normalizeVersion(right);

    if (normalizedLeft === normalizedRight) {
      return 0;
    }

    return normalizedLeft.localeCompare(normalizedRight, undefined, {
      numeric: true,
    });
  }

  for (const key of ["major", "minor", "patch"] as const) {
    if (parsedLeft[key] > parsedRight[key]) {
      return 1;
    }
    if (parsedLeft[key] < parsedRight[key]) {
      return -1;
    }
  }

  if (parsedLeft.prerelease.length === 0 && parsedRight.prerelease.length === 0) {
    return 0;
  }

  if (parsedLeft.prerelease.length === 0) {
    return 1;
  }

  if (parsedRight.prerelease.length === 0) {
    return -1;
  }

  const maxLength = Math.max(
    parsedLeft.prerelease.length,
    parsedRight.prerelease.length,
  );

  for (let index = 0; index < maxLength; index += 1) {
    const leftPart = parsedLeft.prerelease[index];
    const rightPart = parsedRight.prerelease[index];

    if (leftPart === undefined) {
      return -1;
    }

    if (rightPart === undefined) {
      return 1;
    }

    const result = comparePrereleaseIdentifiers(leftPart, rightPart);
    if (result !== 0) {
      return result;
    }
  }

  return 0;
}

export function isPrereleaseVersion(value: string): boolean {
  return (parseVersion(value)?.prerelease.length || 0) > 0;
}

export function getReleaseTagForVersion(value: string): string {
  const normalized = normalizeVersion(value);
  return normalized.startsWith("v") ? normalized : `v${normalized}`;
}
