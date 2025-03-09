import * as d3 from "https://cdn.jsdelivr.net/npm/d3@7/+esm";

export function createMarginalHistogram(
  data,
  {
    width = 200,
    height = 600,
    margin = { top: 20, right: 20, bottom: 30, left: 40 }, // Match time series margins
    bins = 30,
    yDomain = null,
    yRange = [height - margin.bottom, margin.top], // Default yRange based on height and margins
  } = {}
) {
  // Create container div
  const container = document.createElement("div");
  container.style.position = "relative";
  container.style.height = "100%";
  container.style.width = "100%";
  container.style.padding = "0";
  container.style.margin = "0";

  // Create SVG
  const svg = d3
    .create("svg")
    .attr("width", width)
    .attr("height", height)
    .attr("viewBox", [0, 0, width, height])
    .style("background", "transparent");

  // Extract prices
  const bidPrices = data.map((d) => d.bid_price);
  const askPrices = data.map((d) => d.ask_price);

  // Use exactly the same domain and range as the line chart
  const yScale = d3.scaleLinear().domain(yDomain).range(yRange);

  // Create histogram generator
  const histogram = d3
    .histogram()
    .domain(yScale.domain())
    .thresholds(yScale.ticks(bins));

  // Generate histogram data (only for bid and ask)
  const bidHistogram = histogram(bidPrices);
  const askHistogram = histogram(askPrices);

  // Find max count for x scale (only using bid and ask histograms)
  const maxCount = Math.max(
    d3.max(bidHistogram, (d) => d.length),
    d3.max(askHistogram, (d) => d.length)
  );

  const xScale = d3
    .scaleLinear()
    .domain([0, maxCount * 1.2])
    .range([0, width - margin.left - margin.right]);

  // Calculate means and standard deviations
  const bidMean = d3.mean(bidPrices);
  const askMean = d3.mean(askPrices);
  const bidStd = d3.deviation(bidPrices);
  const askStd = d3.deviation(askPrices);

  // Generate normal distribution points with smaller step size for smoother curves
  const normalPoints = d3.range(yScale.domain()[0], yScale.domain()[1], 0.01);

  // Add normal distribution curves
  const lineGenerator = d3
    .line()
    .x((d) => xScale(d.density))
    .y((d) => yScale(d.price))
    .curve(d3.curveBasis);

  // Calculate normal distributions and scale them
  function getNormalPoints(mean, std) {
    const points = normalPoints.map((price) => ({
      price,
      density:
        (1 / (std * Math.sqrt(2 * Math.PI))) *
        Math.exp(-((price - mean) ** 2) / (2 * std ** 2)),
    }));

    // Scale the densities so the peak reaches maxCount
    const maxDensity = Math.max(...points.map((p) => p.density));
    return points.map((p) => ({
      price: p.price,
      density: (p.density / maxDensity) * maxCount * 1.2,
    }));
  }

  // Bid normal curve
  const bidNormalPoints = getNormalPoints(bidMean, bidStd);
  svg
    .append("path")
    .datum(bidNormalPoints)
    .attr("fill", "none")
    .attr("stroke", "#ef4444")
    .attr("stroke-width", 2)
    .attr("d", lineGenerator)
    .attr("transform", `translate(${margin.left},0)`);

  // Ask normal curve
  const askNormalPoints = getNormalPoints(askMean, askStd);
  svg
    .append("path")
    .datum(askNormalPoints)
    .attr("fill", "none")
    .attr("stroke", "#22c55e")
    .attr("stroke-width", 2)
    .attr("d", lineGenerator)
    .attr("transform", `translate(${margin.left},0)`);

  // Add mean lines
  svg
    .append("line")
    .attr("x1", margin.left)
    .attr("x2", width - margin.right)
    .attr("y1", yScale(bidMean))
    .attr("y2", yScale(bidMean))
    .attr("stroke", "#ef4444")
    .attr("stroke-width", 3)
    .attr("opacity", 0.8)
    .attr("stroke-dasharray", "4,4");

  svg
    .append("line")
    .attr("x1", margin.left)
    .attr("x2", width - margin.right)
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
    .attr("x1", margin.left)
    .attr("x2", margin.left + 40)
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
    .attr("x1", margin.left)
    .attr("x2", margin.left + 40)
    .attr("y1", (d) => yScale(d.ask_price))
    .attr("y2", (d) => yScale(d.ask_price))
    .attr("stroke", "#22c55e")
    .attr("stroke-width", 3)
    .attr("opacity", (d, i) => ((i + 1) / lastN) * 0.7);

  // Create bars for bid prices with more transparency
  svg
    .append("g")
    .attr("transform", `translate(${margin.left}, 0)`)
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
    .attr("transform", `translate(${margin.left}, 0)`)
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
