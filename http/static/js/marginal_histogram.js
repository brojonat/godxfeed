import * as d3 from "https://cdn.jsdelivr.net/npm/d3@7/+esm";

export function createMarginalHistogram(
  data,
  { width = 200, height = 600, padding = 20, bins = 30, yDomain = null } = {}
) {
  // Create container div
  const container = document.createElement("div");
  container.style.position = "relative";
  container.style.height = "100%";
  container.style.width = "100%";

  // Create SVG
  const svg = d3
    .create("svg")
    .attr("width", width)
    .attr("height", height)
    .attr("viewBox", [0, 0, width, height])
    .style("background", "transparent");

  // Extract bid and ask prices
  const bidPrices = data.map((d) => d.bid_price);
  const askPrices = data.map((d) => d.ask_price);

  // Calculate domain with padding
  const allPrices = [...bidPrices, ...askPrices];
  const minPrice = Math.min(...allPrices);
  const maxPrice = Math.max(...allPrices);
  const pricePadding = (maxPrice - minPrice) * 0.1;

  // Use provided domain if available, otherwise calculate it
  const yScale = d3
    .scaleLinear()
    .domain(yDomain || [minPrice - pricePadding, maxPrice + pricePadding])
    .range([height - padding, padding]);

  // Create histogram generator
  const histogram = d3
    .histogram()
    .domain(yScale.domain())
    .thresholds(yScale.ticks(bins));

  // Generate histogram data
  const bidHistogram = histogram(bidPrices);
  const askHistogram = histogram(askPrices);

  // Find max count for x scale
  const maxCount = Math.max(
    d3.max(bidHistogram, (d) => d.length),
    d3.max(askHistogram, (d) => d.length)
  );

  const xScale = d3
    .scaleLinear()
    .domain([0, maxCount * 1.2])
    .range([0, width - padding * 2]);

  // Add normal distribution curves
  const normalLine = d3
    .line()
    .x((d) => xScale(d.density))
    .y((d) => yScale(d.value));

  // Generate points for bid prices normal distribution curve
  const bidMean = d3.mean(bidPrices);
  const bidStdDev = d3.deviation(bidPrices);
  const bidMaxHeight = d3.max(bidHistogram, (d) => d.length);

  const bidCurvePoints = d3
    .range(minPrice - pricePadding, maxPrice + pricePadding, 0.01)
    .map((x) => ({
      value: x,
      density: bidMaxHeight * Math.exp(-0.5 * ((x - bidMean) / bidStdDev) ** 2),
    }));

  // Generate points for ask prices normal distribution curve
  const askMean = d3.mean(askPrices);
  const askStdDev = d3.deviation(askPrices);
  const askMaxHeight = d3.max(askHistogram, (d) => d.length);

  const askCurvePoints = d3
    .range(minPrice - pricePadding, maxPrice + pricePadding, 0.01)
    .map((x) => ({
      value: x,
      density: askMaxHeight * Math.exp(-0.5 * ((x - askMean) / askStdDev) ** 2),
    }));

  // Add the bid curve
  svg
    .append("path")
    .datum(bidCurvePoints)
    .attr("transform", `translate(${padding}, 0)`)
    .attr("fill", "none")
    .attr("stroke", "#ef4444")
    .attr("stroke-width", 3.5)
    .attr("opacity", 0.8)
    .attr("d", normalLine);

  // Add the ask curve
  svg
    .append("path")
    .datum(askCurvePoints)
    .attr("transform", `translate(${padding}, 0)`)
    .attr("fill", "none")
    .attr("stroke", "#22c55e")
    .attr("stroke-width", 3.5)
    .attr("opacity", 0.8)
    .attr("d", normalLine);

  // Add mean lines
  svg
    .append("line")
    .attr("x1", padding)
    .attr("x2", width - padding)
    .attr("y1", yScale(bidMean))
    .attr("y2", yScale(bidMean))
    .attr("stroke", "#ef4444")
    .attr("stroke-width", 3)
    .attr("opacity", 0.8)
    .attr("stroke-dasharray", "4,4");

  svg
    .append("line")
    .attr("x1", padding)
    .attr("x2", width - padding)
    .attr("y1", yScale(askMean))
    .attr("y2", yScale(askMean))
    .attr("stroke", "#22c55e")
    .attr("stroke-width", 3)
    .attr("opacity", 0.8)
    .attr("stroke-dasharray", "4,4");

  // Add last N bid and ask price indicators
  const lastN = 5;
  const lastBids = data.slice(-lastN);
  const lastAsks = data.slice(-lastN);

  // Add horizontal lines for recent bid prices
  svg
    .selectAll(".vline-bid")
    .data(lastBids)
    .join("line")
    .attr("class", "vline-bid")
    .attr("x1", padding) // Start from left padding
    .attr("x2", padding + 40) // Extend 40px to the right
    .attr("y1", (d) => yScale(d.bid_price))
    .attr("y2", (d) => yScale(d.bid_price))
    .attr("stroke", "#ef4444")
    .attr("stroke-width", 3)
    .attr("opacity", (d, i) => ((i + 1) / lastN) * 0.7);

  // Add horizontal lines for recent ask prices
  svg
    .selectAll(".vline-ask")
    .data(lastAsks)
    .join("line")
    .attr("class", "vline-ask")
    .attr("x1", padding) // Start from left padding
    .attr("x2", padding + 40) // Extend 40px to the right
    .attr("y1", (d) => yScale(d.ask_price))
    .attr("y2", (d) => yScale(d.ask_price))
    .attr("stroke", "#22c55e")
    .attr("stroke-width", 3)
    .attr("opacity", (d, i) => ((i + 1) / lastN) * 0.7);

  // Create bars for bid prices with more transparency
  svg
    .append("g")
    .attr("transform", `translate(${padding}, 0)`)
    .selectAll("rect.bid")
    .data(bidHistogram)
    .join("rect")
    .attr("class", "bid")
    .attr("y", (d) => yScale(d.x1))
    .attr("x", 0)
    .attr("height", (d) => yScale(d.x0) - yScale(d.x1))
    .attr("width", (d) => xScale(d.length))
    .attr("fill", "#ef4444")
    .attr("opacity", 0.2);

  // Create bars for ask prices with more transparency
  svg
    .append("g")
    .attr("transform", `translate(${padding}, 0)`)
    .selectAll("rect.ask")
    .data(askHistogram)
    .join("rect")
    .attr("class", "ask")
    .attr("y", (d) => yScale(d.x1))
    .attr("x", 0)
    .attr("height", (d) => yScale(d.x0) - yScale(d.x1))
    .attr("width", (d) => xScale(d.length))
    .attr("fill", "#22c55e")
    .attr("opacity", 0.2);

  // Create text div
  const textDiv = document.createElement("div");
  textDiv.style.position = "absolute"; // Position absolutely
  textDiv.style.top = "20px"; // Position at top
  textDiv.style.right = "10px"; // Position at right
  textDiv.style.color = "#e2e8f0";
  textDiv.style.fontSize = "14px";
  textDiv.style.fontWeight = "600";
  textDiv.style.whiteSpace = "nowrap";

  // Calculate spreads
  const meanSpread = askMean - bidMean;
  const currentSpread =
    data[data.length - 1].ask_price - data[data.length - 1].bid_price;

  textDiv.innerHTML = `
    <div>Spreads:</div>
    <div style="margin-top: 5px">Avg: ${meanSpread.toFixed(3)}</div>
    <div>Last: ${currentSpread.toFixed(3)}</div>
  `;

  // Add SVG and text div to container
  container.appendChild(svg.node());
  container.appendChild(textDiv);

  return container;
}
