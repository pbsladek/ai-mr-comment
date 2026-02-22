"use strict";

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

module.exports = function scorePRQuality(output, context) {
  const vars = (context && context.vars) || {};
  const minScore = Number(vars.min_score || 0.75);
  const maxTitleLength = Number(vars.max_title_length || 100);

  let parsed;
  try {
    parsed = JSON.parse(output);
  } catch (err) {
    return {
      pass: false,
      score: 0,
      reason: `expected JSON PR output, got parse error: ${err.message}`,
    };
  }

  const title = normalize(parsed.title);
  const description = normalize(parsed.description || parsed.comment);
  if (!title || !description) {
    return {
      pass: false,
      score: 0,
      reason: "missing title and/or description",
    };
  }

  const titleLower = title.toLowerCase();
  const descLower = description.toLowerCase();
  const combined = `${titleLower}\n${descLower}`;
  const expectedTitleKeywords = toLowerList(vars.expected_title_keywords);
  const expectedDescriptionKeywords = toLowerList(
    vars.expected_description_keywords,
  );
  const forbiddenTerms = toLowerList(vars.forbidden_terms);

  const checks = [];
  checks.push({
    name: "title_single_line",
    ok: !/[\r\n]/.test(title),
    weight: 0.2,
  });
  checks.push({
    name: "title_length",
    ok: title.length <= maxTitleLength,
    weight: 0.15,
  });
  checks.push({
    name: "title_keywords",
    ok:
      expectedTitleKeywords.length === 0 ||
      expectedTitleKeywords.some((kw) => titleLower.includes(kw)),
    weight: 0.2,
  });
  checks.push({
    name: "description_keywords",
    ok:
      expectedDescriptionKeywords.length === 0 ||
      expectedDescriptionKeywords.filter((kw) => descLower.includes(kw))
        .length >= 2,
    weight: 0.2,
  });
  checks.push({
    name: "description_sections",
    ok: /##\s+/m.test(description),
    weight: 0.15,
  });
  checks.push({
    name: "forbidden_terms",
    ok: forbiddenTerms.every((term) => !combined.includes(term)),
    weight: 0.1,
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
    reason: `score=${score.toFixed(3)} | min=${minScore.toFixed(3)} | title="${title}" | failed=${failed.join(",") || "none"}`,
  };
};
