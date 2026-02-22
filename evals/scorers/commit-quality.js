"use strict";

const CONVENTIONAL_RE =
  /^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\([^)]+\))?!?:\s+\S.+$/i;

function normalize(value) {
  if (value === null || value === undefined) {
    return "";
  }
  return String(value).trim();
}

function toLowerList(value) {
  if (typeof value === "string") {
    return value
      .split("|")
      .map((v) => normalize(v).toLowerCase())
      .filter(Boolean);
  }
  if (!Array.isArray(value)) {
    return [];
  }
  return value.map((v) => normalize(v).toLowerCase()).filter(Boolean);
}

module.exports = function scoreCommitQuality(output, context) {
  const vars = (context && context.vars) || {};
  const minScore = Number(vars.min_score || 0.75);
  const maxLength = Number(vars.max_length || 72);
  const requireConventional = vars.require_conventional !== false;

  let parsed;
  try {
    parsed = JSON.parse(output);
  } catch (err) {
    return {
      pass: false,
      score: 0,
      reason: `expected JSON commit output, got parse error: ${err.message}`,
    };
  }

  const message = normalize(
    parsed.commit_message || parsed.commit || parsed.message,
  );
  if (!message) {
    return {
      pass: false,
      score: 0,
      reason: "missing commit_message",
    };
  }

  const msgLower = message.toLowerCase();
  const expectedKeywords = toLowerList(vars.expected_keywords);
  const forbiddenTerms = toLowerList(vars.forbidden_terms);

  const checks = [];
  checks.push({
    name: "single_line",
    ok: !/[\r\n]/.test(message),
    weight: 0.2,
  });
  checks.push({
    name: "max_length",
    ok: message.length <= maxLength,
    weight: 0.2,
  });
  checks.push({
    name: "conventional",
    ok: requireConventional ? CONVENTIONAL_RE.test(message) : true,
    weight: 0.2,
  });
  checks.push({
    name: "expected_keywords",
    ok:
      expectedKeywords.length === 0 ||
      expectedKeywords.some((kw) => msgLower.includes(kw)),
    weight: 0.2,
  });
  checks.push({
    name: "forbidden_terms",
    ok: forbiddenTerms.every((term) => !msgLower.includes(term)),
    weight: 0.2,
  });

  let score = 0;
  const failed = [];
  for (const check of checks) {
    if (check.ok) {
      score += check.weight;
    } else {
      failed.push(check.name);
    }
  }

  score = Number(score.toFixed(3));
  return {
    pass: score >= minScore,
    score,
    reason: `score=${score.toFixed(3)} | min=${minScore.toFixed(3)} | message="${message}" | failed=${failed.join(",") || "none"}`,
  };
};
