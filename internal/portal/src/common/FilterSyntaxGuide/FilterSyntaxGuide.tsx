import Markdown from "react-markdown";

const FILTER_SYNTAX_GUIDE = `# Filter Syntax Guide

Only events matching the filter will be delivered. Filters use JSON to define matching conditions on your event data.

## Event Structure

Events contain two top-level properties that can be filtered:

- \`data\` — The event payload
- \`metadata\` — Additional event information

Your filter should specify one or both of these at the top level:

\`\`\`json
{
  "data": {
    "type": "order.created"
  },
  "metadata": {
    "source": "api"
  }
}
\`\`\`

## Exact Match

Specify the field path and value to match:

\`\`\`json
{
  "data": {
    "type": "order.created"
  }
}
\`\`\`

Multiple conditions are combined with AND logic:

\`\`\`json
{
  "data": {
    "type": "order.created",
    "status": "paid"
  }
}
\`\`\`

## Arrays

Arrays match if they **contain** the specified value(s):

\`\`\`json
{
  "data": {
    "tags": "urgent"
  }
}
\`\`\`

Matches events like: \`{ "tags": ["urgent", "support"] }\`

## Operators

Use operators for complex matching:

**Comparison**
- \`$eq\` — Equals
- \`$neq\` — Not equals
- \`$gt\` — Greater than
- \`$gte\` — Greater or equal
- \`$lt\` — Less than
- \`$lte\` — Less or equal

**Membership**
- \`$in\` — Value in array, or substring match
- \`$nin\` — Value not in array

**String**
- \`$startsWith\` — String starts with
- \`$endsWith\` — String ends with

**Other**
- \`$exist\` — Field exists (\`true\`) or doesn't (\`false\`)

**Example:** Match orders over $100

\`\`\`json
{
  "data": {
    "amount": { "$gte": 100 }
  }
}
\`\`\`

**Example:** Match specific event types

\`\`\`json
{
  "data": {
    "type": {
      "$in": ["order.created", "order.updated"]
    }
  }
}
\`\`\`

**Example:** Match emails ending with a domain

\`\`\`json
{
  "data": {
    "email": { "$endsWith": "@example.com" }
  }
}
\`\`\`

## Combining Operators

Multiple operators on the same field are combined with AND:

\`\`\`json
{
  "data": {
    "amount": {
      "$gte": 100,
      "$lt": 500
    }
  }
}
\`\`\`

## Logical Operators

**\`$or\`** — Match any condition

\`\`\`json
{
  "$or": [
    { "data": { "type": "order.created" } },
    { "data": { "type": "order.updated" } }
  ]
}
\`\`\`

**\`$and\`** — Match all conditions (explicit)

\`\`\`json
{
  "$and": [
    { "data": { "status": "active" } },
    { "data": { "amount": { "$gte": 100 } } }
  ]
}
\`\`\`

**\`$not\`** — Negate a condition

\`\`\`json
{
  "data": {
    "status": {
      "$not": {
        "$in": ["cancelled", "refunded"]
      }
    }
  }
}
\`\`\`

## Field Existence

Check if a field exists (or doesn't):

\`\`\`json
{
  "data": {
    "metadata": { "$exist": true }
  }
}
\`\`\`

\`\`\`json
{
  "data": {
    "deleted_at": { "$exist": false }
  }
}
\`\`\`
`;

export const FilterSyntaxGuide = () => {
  return <Markdown>{FILTER_SYNTAX_GUIDE}</Markdown>;
};

export default FilterSyntaxGuide;
