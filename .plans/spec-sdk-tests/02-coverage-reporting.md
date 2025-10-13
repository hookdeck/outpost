# Coverage Reporting Plan

## Overview

Build automated tooling to track which OpenAPI endpoints are tested, generate coverage reports, and enforce minimum coverage thresholds in CI/CD.

## Goals

1. Identify which OpenAPI endpoints are tested vs. untested
2. Generate visual coverage reports (JSON, HTML, Markdown)
3. Track coverage trends over time
4. Enforce minimum coverage thresholds (e.g., 85%)
5. Integrate coverage data into CI/CD pipeline
6. Provide actionable insights for improving coverage

## Requirements

### Coverage Metrics

Track the following metrics:
- **Endpoint coverage**: % of OpenAPI paths tested
- **Method coverage**: % of HTTP methods (GET, POST, PUT, DELETE) tested
- **Parameter coverage**: % of required parameters validated
- **Response code coverage**: % of documented response codes tested
- **Destination type coverage**: % of tests per destination type

### Report Formats

Generate reports in multiple formats:
- **JSON**: Machine-readable for CI/CD integration
- **HTML**: Visual dashboard for human review
- **Markdown**: Embedded in repository (README, PR comments)
- **Badge**: Dynamic coverage badge for README

## Technical Approach

### 1. Extract Tested Endpoints from Test Files

**Script**: `spec-sdk-tests/scripts/extract-tested-endpoints.ts`

```typescript
import * as fs from 'fs';
import * as path from 'path';
import * as glob from 'glob';

interface TestedEndpoint {
  method: string;
  path: string;
  testFile: string;
  testName: string;
  destinationType: string;
  line: number;
}

interface EndpointPattern {
  pattern: RegExp;
  method: string;
  pathTemplate: string;
}

/**
 * Extract tested endpoints by analyzing test files
 */
export class EndpointExtractor {
  private endpoints: TestedEndpoint[] = [];

  // Patterns to identify SDK method calls that correspond to API endpoints
  private readonly patterns: EndpointPattern[] = [
    // Tenant endpoints
    { pattern: /\.tenants\.create\(/g, method: 'POST', pathTemplate: '/tenants' },
    { pattern: /\.tenants\.get\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}' },
    { pattern: /\.tenants\.list\(/g, method: 'GET', pathTemplate: '/tenants' },
    { pattern: /\.tenants\.update\(/g, method: 'PUT', pathTemplate: '/tenants/{tenant_id}' },
    { pattern: /\.tenants\.delete\(/g, method: 'DELETE', pathTemplate: '/tenants/{tenant_id}' },
    
    // Destination endpoints
    { pattern: /\.destinations\.create\(/g, method: 'POST', pathTemplate: '/tenants/{tenant_id}/destinations' },
    { pattern: /\.destinations\.get\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/destinations/{destination_id}' },
    { pattern: /\.destinations\.list\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/destinations' },
    { pattern: /\.destinations\.update\(/g, method: 'PUT', pathTemplate: '/tenants/{tenant_id}/destinations/{destination_id}' },
    { pattern: /\.destinations\.delete\(/g, method: 'DELETE', pathTemplate: '/tenants/{tenant_id}/destinations/{destination_id}' },
    
    // Topic endpoints
    { pattern: /\.topics\.create\(/g, method: 'POST', pathTemplate: '/tenants/{tenant_id}/topics' },
    { pattern: /\.topics\.get\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/topics/{topic_id}' },
    { pattern: /\.topics\.list\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/topics' },
    { pattern: /\.topics\.delete\(/g, method: 'DELETE', pathTemplate: '/tenants/{tenant_id}/topics/{topic_id}' },
    
    // Event endpoints
    { pattern: /\.events\.publish\(/g, method: 'POST', pathTemplate: '/tenants/{tenant_id}/events' },
    { pattern: /\.events\.get\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/events/{event_id}' },
    
    // Retry endpoints
    { pattern: /\.retries\.retry\(/g, method: 'POST', pathTemplate: '/tenants/{tenant_id}/events/{event_id}/retry' },
    
    // Log endpoints
    { pattern: /\.logs\.list\(/g, method: 'GET', pathTemplate: '/tenants/{tenant_id}/logs' },
  ];

  async extract(testDirectory: string): Promise<TestedEndpoint[]> {
    const testFiles = glob.sync('**/*.test.ts', { cwd: testDirectory });

    for (const file of testFiles) {
      const filePath = path.join(testDirectory, file);
      const content = fs.readFileSync(filePath, 'utf-8');
      const lines = content.split('\n');

      // Extract destination type from file path
      const destinationType = this.extractDestinationType(file);

      // Find all test blocks
      const testMatches = content.matchAll(/(?:it|test)\(['"](.+?)['"]/g);
      
      for (const match of testMatches) {
        const testName = match[1];
        const testStart = match.index || 0;
        const line = content.substring(0, testStart).split('\n').length;

        // Find SDK calls within this test
        for (const pattern of this.patterns) {
          const methodCalls = content.substring(testStart).matchAll(pattern.pattern);
          
          for (const _call of methodCalls) {
            this.endpoints.push({
              method: pattern.method,
              path: pattern.pathTemplate,
              testFile: file,
              testName,
              destinationType,
              line
            });
          }
        }
      }
    }

    return this.endpoints;
  }

  private extractDestinationType(filePath: string): string {
    const match = filePath.match(/destinations\/(.+?)\.test\.ts/);
    return match ? match[1] : 'unknown';
  }

  getUniqueEndpoints(): Array<{ method: string; path: string }> {
    const unique = new Map<string, { method: string; path: string }>();
    
    for (const endpoint of this.endpoints) {
      const key = `${endpoint.method} ${endpoint.path}`;
      unique.set(key, { method: endpoint.method, path: endpoint.path });
    }
    
    return Array.from(unique.values());
  }
}
```

### 2. Parse OpenAPI Specification

**Script**: `spec-sdk-tests/scripts/parse-openapi.ts`

```typescript
import * as fs from 'fs';
import * as yaml from 'js-yaml';

interface OpenAPIEndpoint {
  method: string;
  path: string;
  operationId?: string;
  tags?: string[];
  summary?: string;
  parameters?: any[];
  responses?: Record<string, any>;
  deprecated?: boolean;
}

/**
 * Parse OpenAPI spec and extract all documented endpoints
 */
export class OpenAPIParser {
  private spec: any;
  private endpoints: OpenAPIEndpoint[] = [];

  constructor(specPath: string) {
    const content = fs.readFileSync(specPath, 'utf-8');
    this.spec = yaml.load(content);
  }

  parse(): OpenAPIEndpoint[] {
    const paths = this.spec.paths || {};

    for (const [path, pathItem] of Object.entries(paths)) {
      const methods = ['get', 'post', 'put', 'delete', 'patch'];

      for (const method of methods) {
        const operation = (pathItem as any)[method];
        
        if (operation) {
          this.endpoints.push({
            method: method.toUpperCase(),
            path,
            operationId: operation.operationId,
            tags: operation.tags,
            summary: operation.summary,
            parameters: operation.parameters,
            responses: operation.responses,
            deprecated: operation.deprecated
          });
        }
      }
    }

    return this.endpoints;
  }

  getEndpoints(): OpenAPIEndpoint[] {
    return this.endpoints;
  }

  getNonDeprecatedEndpoints(): OpenAPIEndpoint[] {
    return this.endpoints.filter(e => !e.deprecated);
  }
}
```

### 3. Calculate Coverage

**Script**: `spec-sdk-tests/scripts/calculate-coverage.ts`

```typescript
import { EndpointExtractor } from './extract-tested-endpoints';
import { OpenAPIParser } from './parse-openapi';

interface CoverageReport {
  timestamp: string;
  summary: {
    totalEndpoints: number;
    testedEndpoints: number;
    untestedEndpoints: number;
    coveragePercentage: number;
    deprecatedEndpoints: number;
  };
  byDestinationType: Record<string, {
    totalTests: number;
    testedEndpoints: number;
  }>;
  testedEndpoints: Array<{
    method: string;
    path: string;
    testCount: number;
    destinations: string[];
  }>;
  untestedEndpoints: Array<{
    method: string;
    path: string;
    operationId?: string;
    tags?: string[];
  }>;
  coverageTrend?: Array<{
    date: string;
    coverage: number;
  }>;
}

export class CoverageCalculator {
  async calculate(testDir: string, specPath: string): Promise<CoverageReport> {
    // Extract tested endpoints
    const extractor = new EndpointExtractor();
    const testedEndpoints = await extractor.extract(testDir);
    const uniqueTested = extractor.getUniqueEndpoints();

    // Parse OpenAPI spec
    const parser = new OpenAPIParser(specPath);
    const allEndpoints = parser.parse();
    const nonDeprecated = parser.getNonDeprecatedEndpoints();

    // Calculate coverage
    const testedSet = new Set(
      uniqueTested.map(e => `${e.method} ${e.path}`)
    );

    const tested = nonDeprecated.filter(e => 
      testedSet.has(`${e.method} ${e.path}`)
    );

    const untested = nonDeprecated.filter(e =>
      !testedSet.has(`${e.method} ${e.path}`)
    );

    // Group by destination type
    const byDestination: Record<string, any> = {};
    for (const endpoint of testedEndpoints) {
      if (!byDestination[endpoint.destinationType]) {
        byDestination[endpoint.destinationType] = {
          totalTests: 0,
          testedEndpoints: 0
        };
      }
      byDestination[endpoint.destinationType].totalTests++;
    }

    // Count unique endpoints per destination
    for (const dest of Object.keys(byDestination)) {
      const destEndpoints = testedEndpoints.filter(e => e.destinationType === dest);
      const unique = new Set(destEndpoints.map(e => `${e.method} ${e.path}`));
      byDestination[dest].testedEndpoints = unique.size;
    }

    // Build tested endpoint details
    const testedDetails = uniqueTested.map(endpoint => {
      const tests = testedEndpoints.filter(e =>
        e.method === endpoint.method && e.path === endpoint.path
      );
      
      return {
        method: endpoint.method,
        path: endpoint.path,
        testCount: tests.length,
        destinations: [...new Set(tests.map(t => t.destinationType))]
      };
    });

    return {
      timestamp: new Date().toISOString(),
      summary: {
        totalEndpoints: nonDeprecated.length,
        testedEndpoints: tested.length,
        untestedEndpoints: untested.length,
        coveragePercentage: (tested.length / nonDeprecated.length) * 100,
        deprecatedEndpoints: allEndpoints.length - nonDeprecated.length
      },
      byDestinationType: byDestination,
      testedEndpoints: testedDetails,
      untestedEndpoints: untested.map(e => ({
        method: e.method,
        path: e.path,
        operationId: e.operationId,
        tags: e.tags
      }))
    };
  }

  async loadHistoricalCoverage(historyFile: string): Promise<Array<{date: string; coverage: number}>> {
    if (!fs.existsSync(historyFile)) {
      return [];
    }
    
    const content = fs.readFileSync(historyFile, 'utf-8');
    return JSON.parse(content);
  }

  async updateCoverageHistory(
    historyFile: string,
    coverage: number,
    maxEntries: number = 90
  ): Promise<void> {
    const history = await this.loadHistoricalCoverage(historyFile);
    
    history.push({
      date: new Date().toISOString().split('T')[0],
      coverage: Math.round(coverage * 100) / 100
    });

    // Keep only last N entries
    const trimmed = history.slice(-maxEntries);
    
    fs.writeFileSync(historyFile, JSON.stringify(trimmed, null, 2));
  }
}
```

### 4. Generate Reports

**Script**: `spec-sdk-tests/scripts/generate-reports.ts`

```typescript
import * as fs from 'fs';
import { CoverageCalculator } from './calculate-coverage';

export class ReportGenerator {
  async generate(testDir: string, specPath: string, outputDir: string): Promise<void> {
    const calculator = new CoverageCalculator();
    const report = await calculator.calculate(testDir, specPath);

    // Ensure output directory exists
    fs.mkdirSync(outputDir, { recursive: true });

    // Generate JSON report
    this.generateJSON(report, outputDir);

    // Generate Markdown report
    this.generateMarkdown(report, outputDir);

    // Generate HTML report
    this.generateHTML(report, outputDir);

    // Update coverage history
    await calculator.updateCoverageHistory(
      `${outputDir}/coverage-history.json`,
      report.summary.coveragePercentage
    );

    console.log(`Coverage: ${report.summary.coveragePercentage.toFixed(2)}%`);
    console.log(`Reports generated in ${outputDir}`);
  }

  private generateJSON(report: any, outputDir: string): void {
    fs.writeFileSync(
      `${outputDir}/coverage.json`,
      JSON.stringify(report, null, 2)
    );
  }

  private generateMarkdown(report: any, outputDir: string): void {
    const { summary, testedEndpoints, untestedEndpoints, byDestinationType } = report;

    let md = `# OpenAPI Endpoint Coverage Report\n\n`;
    md += `Generated: ${new Date(report.timestamp).toLocaleString()}\n\n`;

    // Summary
    md += `## Summary\n\n`;
    md += `- **Total Endpoints**: ${summary.totalEndpoints}\n`;
    md += `- **Tested**: ${summary.testedEndpoints} (${summary.coveragePercentage.toFixed(2)}%)\n`;
    md += `- **Untested**: ${summary.untestedEndpoints}\n`;
    md += `- **Deprecated**: ${summary.deprecatedEndpoints}\n\n`;

    // Coverage badge
    const color = summary.coveragePercentage >= 90 ? 'brightgreen' :
                  summary.coveragePercentage >= 75 ? 'green' :
                  summary.coveragePercentage >= 60 ? 'yellow' : 'red';
    md += `![Coverage](https://img.shields.io/badge/coverage-${summary.coveragePercentage.toFixed(0)}%25-${color})\n\n`;

    // By destination type
    md += `## Coverage by Destination Type\n\n`;
    md += `| Destination | Tests | Unique Endpoints |\n`;
    md += `|-------------|------:|------------------:|\n`;
    for (const [dest, stats] of Object.entries(byDestinationType) as any) {
      md += `| ${dest} | ${stats.totalTests} | ${stats.testedEndpoints} |\n`;
    }
    md += `\n`;

    // Tested endpoints
    md += `## Tested Endpoints (${testedEndpoints.length})\n\n`;
    md += `| Method | Path | Tests | Destinations |\n`;
    md += `|--------|------|------:|--------------||\n`;
    for (const endpoint of testedEndpoints) {
      md += `| ${endpoint.method} | ${endpoint.path} | ${endpoint.testCount} | ${endpoint.destinations.join(', ')} |\n`;
    }
    md += `\n`;

    // Untested endpoints
    if (untestedEndpoints.length > 0) {
      md += `## Untested Endpoints (${untestedEndpoints.length})\n\n`;
      md += `| Method | Path | Operation ID | Tags |\n`;
      md += `|--------|------|--------------|------|\n`;
      for (const endpoint of untestedEndpoints) {
        md += `| ${endpoint.method} | ${endpoint.path} | ${endpoint.operationId || '-'} | ${endpoint.tags?.join(', ') || '-'} |\n`;
      }
    }

    fs.writeFileSync(`${outputDir}/coverage.md`, md);
  }

  private generateHTML(report: any, outputDir: string): void {
    const { summary, testedEndpoints, untestedEndpoints } = report;

    const html = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>OpenAPI Coverage Report</title>
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; max-width: 1200px; margin: 0 auto; padding: 20px; }
    h1 { color: #333; }
    .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 20px; margin: 30px 0; }
    .metric { background: #f5f5f5; padding: 20px; border-radius: 8px; text-align: center; }
    .metric-value { font-size: 36px; font-weight: bold; color: #0066cc; }
    .metric-label { font-size: 14px; color: #666; margin-top: 8px; }
    table { width: 100%; border-collapse: collapse; margin: 20px 0; }
    th, td { text-align: left; padding: 12px; border-bottom: 1px solid #ddd; }
    th { background: #f5f5f5; font-weight: 600; }
    .method { display: inline-block; padding: 4px 8px; border-radius: 4px; font-size: 12px; font-weight: bold; }
    .GET { background: #61affe; color: white; }
    .POST { background: #49cc90; color: white; }
    .PUT { background: #fca130; color: white; }
    .DELETE { background: #f93e3e; color: white; }
    .progress { width: 100%; height: 30px; background: #f0f0f0; border-radius: 4px; overflow: hidden; }
    .progress-bar { height: 100%; background: linear-gradient(90deg, #4caf50, #8bc34a); transition: width 0.3s; }
  </style>
</head>
<body>
  <h1>OpenAPI Endpoint Coverage Report</h1>
  <p>Generated: ${new Date(report.timestamp).toLocaleString()}</p>

  <div class="summary">
    <div class="metric">
      <div class="metric-value">${summary.coveragePercentage.toFixed(1)}%</div>
      <div class="metric-label">Coverage</div>
    </div>
    <div class="metric">
      <div class="metric-value">${summary.testedEndpoints}</div>
      <div class="metric-label">Tested Endpoints</div>
    </div>
    <div class="metric">
      <div class="metric-value">${summary.untestedEndpoints}</div>
      <div class="metric-label">Untested Endpoints</div>
    </div>
    <div class="metric">
      <div class="metric-value">${summary.totalEndpoints}</div>
      <div class="metric-label">Total Endpoints</div>
    </div>
  </div>

  <div class="progress">
    <div class="progress-bar" style="width: ${summary.coveragePercentage}%"></div>
  </div>

  <h2>Tested Endpoints (${testedEndpoints.length})</h2>
  <table>
    <thead>
      <tr>
        <th>Method</th>
        <th>Path</th>
        <th>Tests</th>
        <th>Destinations</th>
      </tr>
    </thead>
    <tbody>
      ${testedEndpoints.map((e: any) => `
        <tr>
          <td><span class="method ${e.method}">${e.method}</span></td>
          <td><code>${e.path}</code></td>
          <td>${e.testCount}</td>
          <td>${e.destinations.join(', ')}</td>
        </tr>
      `).join('')}
    </tbody>
  </table>

  ${untestedEndpoints.length > 0 ? `
    <h2>Untested Endpoints (${untestedEndpoints.length})</h2>
    <table>
      <thead>
        <tr>
          <th>Method</th>
          <th>Path</th>
          <th>Operation ID</th>
        </tr>
      </thead>
      <tbody>
        ${untestedEndpoints.map((e: any) => `
          <tr>
            <td><span class="method ${e.method}">${e.method}</span></td>
            <td><code>${e.path}</code></td>
            <td>${e.operationId || '-'}</td>
          </tr>
        `).join('')}
      </tbody>
    </table>
  ` : ''}
</body>
</html>`;

    fs.writeFileSync(`${outputDir}/coverage.html`, html);
  }
}
```

### 5. Integration with CI/CD

Add to `spec-sdk-tests/package.json`:

```json
{
  "scripts": {
    "coverage:generate": "ts-node scripts/generate-reports.ts",
    "coverage:check": "ts-node scripts/check-threshold.ts"
  }
}
```

**Script**: `spec-sdk-tests/scripts/check-threshold.ts`

```typescript
import * as fs from 'fs';

const MINIMUM_COVERAGE = 85; // 85% minimum coverage

async function checkThreshold() {
  const reportPath = './coverage-reports/coverage.json';
  
  if (!fs.existsSync(reportPath)) {
    console.error('Coverage report not found');
    process.exit(1);
  }

  const report = JSON.parse(fs.readFileSync(reportPath, 'utf-8'));
  const coverage = report.summary.coveragePercentage;

  console.log(`Current coverage: ${coverage.toFixed(2)}%`);
  console.log(`Minimum required: ${MINIMUM_COVERAGE}%`);

  if (coverage < MINIMUM_COVERAGE) {
    console.error(`❌ Coverage ${coverage.toFixed(2)}% is below threshold ${MINIMUM_COVERAGE}%`);
    process.exit(1);
  }

  console.log(`✅ Coverage meets threshold`);
  process.exit(0);
}

checkThreshold();
```

Add to GitHub Actions workflow:

```yaml
      - name: Generate coverage report
        working-directory: spec-sdk-tests
        run: |
          npm run coverage:generate
          npm run coverage:check

      - name: Upload coverage report
        uses: actions/upload-artifact@v4
        with:
          name: coverage-report
          path: spec-sdk-tests/coverage-reports/

      - name: Comment PR with coverage
        if: github.event_name == 'pull_request'
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const coverage = fs.readFileSync('spec-sdk-tests/coverage-reports/coverage.md', 'utf8');
            
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: coverage
            });
```

### 6. Visualization

**Coverage Trend Chart (using Chart.js):**

Add to HTML report:

```html
<canvas id="coverageTrend" width="400" height="200"></canvas>
<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
<script>
  const history = ${JSON.stringify(report.coverageTrend || [])};
  const ctx = document.getElementById('coverageTrend').getContext('2d');
  new Chart(ctx, {
    type: 'line',
    data: {
      labels: history.map(h => h.date),
      datasets: [{
        label: 'Coverage %',
        data: history.map(h => h.coverage),
        borderColor: '#4caf50',
        tension: 0.1
      }]
    },
    options: {
      scales: {
        y: { beginAtZero: true, max: 100 }
      }
    }
  });
</script>
```

## Acceptance Criteria

- [ ] Script extracts tested endpoints from test files
- [ ] Script parses OpenAPI spec and lists all endpoints
- [ ] Coverage calculator compares tested vs. documented endpoints
- [ ] JSON report generated with detailed coverage data
- [ ] Markdown report generated for repository
- [ ] HTML report generated with visualizations
- [ ] Coverage history tracked over time (90 days)
- [ ] CI/CD enforces minimum 85% coverage threshold
- [ ] PR comments include coverage report
- [ ] Coverage badge displays in README
- [ ] Untested endpoints clearly identified
- [ ] Coverage trends visible in reports

## Dependencies

- [`js-yaml`](https://www.npmjs.com/package/js-yaml) for parsing OpenAPI spec
- [`glob`](https://www.npmjs.com/package/glob) for finding test files
- Chart.js for visualizations (optional)
- GitHub Actions for CI/CD integration

## Risks & Considerations

1. **Endpoint Matching Complexity**
   - Risk: Path parameters make exact matching difficult
   - Mitigation: Normalize paths, use pattern matching

2. **False Positives/Negatives**
   - Risk: May incorrectly identify tested/untested endpoints
   - Mitigation: Manual review, refinement of extraction patterns

3. **Maintenance Overhead**
   - Risk: Extraction patterns need updates as SDK changes
   - Mitigation: Automated tests for coverage script itself

4. **Performance**
   - Risk: Parsing large codebases may be slow
   - Mitigation: Cache results, parallel processing

## Future Enhancements

- Parameter-level coverage (required vs. optional params)
- Response code coverage (2xx, 4xx, 5xx scenarios)
- Schema validation coverage (request/response bodies)
- Interactive coverage dashboard with drill-down
- Coverage diff between branches
- Auto-generate test stubs for untested endpoints

---

**Estimated Effort**: 3-4 days  
**Priority**: High  
**Dependencies**: CI/CD integration (Phase 1)