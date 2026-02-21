'use strict';

const DEFAULT_CLEAN_FORBIDDEN_TERMS = [
  'sql injection',
  'injection vulnerability',
  'xss',
  'cross-site scripting',
  'csrf',
  'race condition',
  'data race',
  'panic',
  'nil pointer',
  'auth bypass',
  'broken authentication',
];

function normalize(value) {
  if (value === null || value === undefined) {
    return '';
  }
  return String(value).toLowerCase();
}

function containsAny(text, terms) {
  for (const term of terms) {
    if (term && text.includes(term)) {
      return true;
    }
  }
  return false;
}

module.exports = function scoreReviewQuality(output, context) {
  let parsed;
  try {
    parsed = JSON.parse(output);
  } catch (err) {
    return {
      pass: false,
      score: 0,
      reason: `expected JSON output from ai-mr-comment, got parse error: ${err.message}`,
    };
  }

  const reviewText = normalize(
    [
      parsed.title || '',
      parsed.description || parsed.comment || '',
      parsed.verdict || '',
    ].join('\n'),
  );

  if (!reviewText.trim()) {
    return {
      pass: false,
      score: 0,
      reason: 'empty review text',
    };
  }

  const vars = context.vars || {};
  const expectedFindings = Array.isArray(vars.expected_findings) ? vars.expected_findings : [];

  let totalWeight = 0;
  let foundWeight = 0;
  let severityWeight = 0;
  let fixWeight = 0;
  const missingFindings = [];

  for (const finding of expectedFindings) {
    const weight = Number(finding.weight || 1);
    const keywords = Array.isArray(finding.keywords) ? finding.keywords.map(normalize) : [];
    const severity = normalize(finding.severity);
    const fixKeywords = Array.isArray(finding.fix_keywords) ? finding.fix_keywords.map(normalize) : [];

    totalWeight += weight;
    const matched = containsAny(reviewText, keywords);
    if (!matched) {
      missingFindings.push(finding.id || 'unnamed_finding');
      continue;
    }

    foundWeight += weight;
    if (!severity || reviewText.includes(severity)) {
      severityWeight += weight;
    }
    if (fixKeywords.length === 0 || containsAny(reviewText, fixKeywords)) {
      fixWeight += weight;
    }
  }

  const recall = totalWeight > 0 ? foundWeight / totalWeight : 1;
  const severityScore = foundWeight > 0 ? severityWeight / foundWeight : 1;
  const actionability = foundWeight > 0 ? fixWeight / foundWeight : 1;

  let precision = 1;
  const precisionNotes = [];

  const forbiddenTerms = Array.isArray(vars.forbidden_terms) ? vars.forbidden_terms.map(normalize) : [];
  if (forbiddenTerms.length > 0) {
    const matchedForbidden = forbiddenTerms.filter((term) => term && reviewText.includes(term));
    if (matchedForbidden.length > 0) {
      precision = 0;
      precisionNotes.push(`forbidden terms present: ${matchedForbidden.join(', ')}`);
    }
  }

  if (vars.should_be_clean === true) {
    const cleanForbiddenTerms = Array.isArray(vars.clean_forbidden_terms)
      ? vars.clean_forbidden_terms.map(normalize)
      : DEFAULT_CLEAN_FORBIDDEN_TERMS;
    const matchedCleanForbidden = cleanForbiddenTerms.filter((term) => term && reviewText.includes(term));
    if (matchedCleanForbidden.length > 0) {
      precision = 0;
      precisionNotes.push(`false-positive bug terms: ${matchedCleanForbidden.join(', ')}`);
    }
    if (reviewText.includes('no material issues') || reviewText.includes('no major issues')) {
      precision = Math.max(precision, 1);
    }
  }

  const score =
    0.45 * recall +
    0.20 * severityScore +
    0.20 * actionability +
    0.15 * precision;

  const minScore = Number(vars.min_score || 0.7);
  const pass = score >= minScore;

  const parts = [
    `score=${score.toFixed(3)}`,
    `min=${minScore.toFixed(3)}`,
    `recall=${recall.toFixed(3)}`,
    `severity=${severityScore.toFixed(3)}`,
    `actionability=${actionability.toFixed(3)}`,
    `precision=${precision.toFixed(3)}`,
  ];
  if (missingFindings.length > 0) {
    parts.push(`missing=${missingFindings.join(',')}`);
  }
  if (precisionNotes.length > 0) {
    parts.push(precisionNotes.join('; '));
  }

  return {
    pass,
    score: Number(score.toFixed(3)),
    reason: parts.join(' | '),
  };
};
