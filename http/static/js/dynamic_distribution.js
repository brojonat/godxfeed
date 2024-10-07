// In an ES6 application

// Example usage:
// const maxQueue = new MaxLengthQueue(3);
// maxQueue.enqueue(1);
// maxQueue.enqueue(2);
// maxQueue.enqueue(3);
// console.log(maxQueue.queue); // Output: [1, 2, 3]

// maxQueue.enqueue(4);
// console.log(maxQueue.queue); // Output: [2, 3, 4] (1 is removed)

// maxQueue.dequeue();
// console.log(maxQueue.queue); // Output: [3, 4]

// console.log(maxQueue.front()); // Output: 3
// console.log(maxQueue.back());  // Output: 4
// console.log(maxQueue.isEmpty()); // Output: false
class MaxLengthQueue {
  constructor(maxLength) {
    this.maxLength = maxLength;
    this.queue = [];
  }

  enqueue(item) {
    if (this.queue.length >= this.maxLength) {
      this.dequeue();
    }
    this.queue.push(item);
  }

  dequeue() {
    if (this.queue.length > 0) {
      return this.queue.shift();
    }
    return null; // Or throw an error depending on how you want to handle empty queue cases
  }

  front() {
    return this.queue.length > 0 ? this.queue[0] : null;
  }

  back() {
    return this.queue.length > 0 ? this.queue[this.queue.length - 1] : null;
  }

  isEmpty() {
    return this.queue.length === 0;
  }

  length() {
    return this.queue.length;
  }
}

function getWS(endpoint) {
  // Create a new WebSocket object
  const socket = new WebSocket(endpoint);

  // Event listener for when the connection is opened
  socket.onopen = function (event) {
    console.log("WebSocket connection opened");

    // subscribe to the random channel
    socket.send(
      JSON.stringify({ type: "subscribe", body: { topic: "TOPIC" } })
    );
  };

  // Event listener for receiving messages from the server
  socket.onmessage = function (event) {
    console.log("you probably forgot to override the onmessage method");
    console.log("Message from server:", event.data);
  };

  // Event listener for when the connection is closed
  socket.onclose = function (event) {
    console.log("WebSocket connection closed");
  };

  // Event listener for errors
  socket.onerror = function (event) {
    console.log(socket);
    console.error("WebSocket error:", event);
  };
  return socket;
}

// this will define a blob of data, then then incrementally push the data into a queue. The
// queue underlies a d3 distribution plot, so we'll see what the plot looks like as it
// updates and dances in real time. Cool? Cool. Hopefully. If this one works, we'll
// generalize to a ridge line and let users dynamically pick the symbols to display.
async function run() {
  const qSize = 500;
  // run the chart setup once
  const chartParams = setupChart(qSize);

  // fixed length data container we can push data into
  const data = new MaxLengthQueue(qSize);

  // subscribe to the server for updates and asynchronously fill the distribution
  const socket = getWS("ws://localhost:8080/ws");
  socket.onmessage = function (event) {
    data.enqueue(JSON.parse(event.data));
    updateChart(chartParams, data);
  };
}

function setupChart(qSize) {
  const svg = d3.select("svg");
  const width = +svg.attr("width");
  const height = +svg.attr("height");
  const margin = { top: 20, right: 20, bottom: 70, left: 40 };
  const innerWidth = width - margin.left - margin.right;
  const innerHeight = height - margin.top - margin.bottom;

  const gwidth = 200;
  const gheight = 200;
  ge = svg
    .append("g")
    .attr("transform", `translate(${width - gwidth},${-gwidth / 2})`);
  svg
    .append("text")
    .attr("id", "gauge-text")
    .attr("class", "gauge-text")
    .attr("text-anchor", "middle")
    .attr("x", width - gwidth + gwidth / 2)
    .attr("y", gheight / 2 + 40)
    // .attr("transform", `translate(${width - gwidth},${gwidth + 50})`)
    .text(`0% (0/${qSize})`);
  const gauge = new window.d3SimpleGauge.SimpleGauge({
    el: ge,
    width: gwidth, // The width of the gauge
    height: gheight, // The height of the gauge
    interval: [0, qSize], // The interval (min and max values) of the gauge (optional)
    sectionsCount: 3, // The number of sections in the gauge
    needleRadius: 10,
    // needleColor: "black", // The needle color
    // sectionsColors: [
    //   // The color of each section
    //   "rgb(255, 0, 0)",
    //   "#ffa500",
    //   "green",
    // ],
  });

  // Create initial scales
  const [min, max] = [0, 10];
  const x = d3
    .scaleLinear()
    .domain([min, max])
    .range([margin.left, width - margin.right]);
  const y = d3
    .scaleLinear()
    .domain([0, 1])
    .range([height - margin.bottom, margin.top]);

  const g = svg
    .append("g")
    .attr("transform", `translate(${margin.left},${margin.top})`);

  // Append axes
  const gx = g
    .append("g")
    .attr("class", "xaxis")
    .attr("transform", `translate(0,${height - margin.bottom})`)
    .call(d3.axisBottom(x));

  svg
    .append("text")
    .attr("class", "x-label")
    .attr("text-anchor", "end")
    .attr("x", width - margin.right)
    .attr("y", height - 10)
    .text("bid price (dollars)");

  const gy = g
    .append("g")
    .attr("class", "yaxis")
    .attr("transform", `translate(${margin.left},0)`)
    .call(d3.axisLeft(y));

  svg
    .append("text")
    .attr("class", "y-label")
    .attr("text-anchor", "end")
    .attr("x", margin.left / 2)
    .attr("y", margin.top)
    .attr("transform", `rotate(-90,${margin.left / 2},${margin.top})`)
    .text("relative frequency (counts)");

  return { width, height, margin, innerWidth, innerHeight, g, gx, gy, gauge };
}

function updateChart(chartParams, data) {
  // update the gauge
  d3.select("#gauge-text").text(
    `${Math.round((data.queue.length / data.maxLength) * 100)}% (${
      data.queue.length
    }/${data.maxLength})`
  );

  chartParams.gauge.value = data.queue.length;
  // Skip cases with 0 or 1 element in data since it'll result in NaNs or a bin
  // with x0 == x1 (i.e., zero width) which throws an error
  if (data.queue.length < 2) {
    return;
  }
  // Create new scales
  const [min, max] = d3.extent(data.queue, (d) => d.value);
  const binCount = 40;
  const thresholds = d3.range(min, max, (max - min) / binCount);

  const x = d3
    .scaleLinear()
    .domain([min, max])
    .range([chartParams.margin.left, chartParams.margin.left + innerWidth]);

  // recompute the histogram
  const histogram = d3
    .histogram()
    .domain(x.domain())
    .value((d) => d.value)
    .thresholds(thresholds);

  const bins = histogram(data.queue);

  // recompute the line
  const line = d3
    .line()
    .curve(d3.curveMonotoneX)
    .x((d) => x((d.x0 + d.x1) / 2))
    .y((d) => y(d.length));

  const y = d3
    .scaleLinear()
    .domain([
      0,
      d3.max(bins, (d) => {
        return d.length;
      }),
    ])
    .range([
      chartParams.margin.top + chartParams.innerHeight,
      chartParams.margin.top,
    ]);

  // Create an area generator
  const area = d3
    .area()
    .curve(d3.curveMonotoneX)
    .x((d) => x((d.x0 + d.x1) / 2))
    .y0(y(0))
    .y1((d) => y(d.length));

  // update axes
  chartParams.gx.transition().duration(10).call(d3.axisBottom(x));
  chartParams.gy.transition().duration(10).call(d3.axisLeft(y));

  // // Draw bars
  chartParams.g
    .selectAll(".bar")
    .data(bins)
    .join(
      (enter) => enter.append("rect"),
      (update) => update,
      (exit) => exit.remove()
    )
    .attr("class", "bar")
    .transition()
    .duration(10)
    .attr("x", (d) => x(d.x0))
    .attr("y", (d) => y(d.length))
    .attr("width", (d) => x(d.x1) - x(d.x0) - 1)
    .attr("height", (d) => y(0) - y(d.length));

  // Create a "padded" version of the bins so that the area and line endpoints
  // call on the ends of the range. This requires adding a half bin on the
  // endpoints of the original bin data.
  const bw = bins[0].x1 - bins[0].x0;
  const paddedBins = [
    {
      x0: bins[0].x0 - bw / 2,
      x1: bins[0].x1 - bw / 2,
      length: bins[0].length,
    },
    ...bins,
    {
      x0: bins[bins.length - 1].x0 + bw / 2,
      x1: bins[bins.length - 1].x1 + bw / 2,
      length: bins[bins.length - 1].length,
    },
  ];

  // Draw curve
  chartParams.g
    .selectAll(".line")
    .data([paddedBins])
    .join(
      (enter) => enter.append("path").attr("class", "line"),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(10)
    .attr("d", line)
    .attr("fill", "none")
    .attr("stroke", "green")
    .attr("stroke-width", 2.5);

  // Draw the area
  chartParams.g
    .selectAll(".fillarea")
    .data([paddedBins])
    .join(
      (enter) => enter.append("path").attr("class", "fillarea"),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(10)
    .attr("d", area)
    .attr("fill", "green")
    .attr("fill-opacity", 0.2);

  // Draw last N prices with increasing opacity
  const lastN = 10;
  chartParams.g
    .selectAll(".vline")
    .data(data.queue.slice(-lastN), (d) => {
      return d.value;
    })
    .join(
      (enter) =>
        // enter selection starts vertically over the destination so the lines
        // flow straight down.
        enter
          .append("line")
          .attr("class", "vline")
          .attr("x1", (d) => x(d.value))
          .attr("x2", (d) => x(d.value))
          .attr("y1", (d) => 0)
          .attr("y2", (d) => 0),
      (update) => update,
      (exit) => exit.remove()
    )
    .transition()
    .duration(80)
    .attr("x1", (d) => x(d.value))
    .attr("y1", (d) => y(0))
    .attr("x2", (d) => x(d.value))
    .attr("y2", (d) => chartParams.margin.top + chartParams.innerHeight - 25)
    .attr("fill", "none")
    .attr("stroke", "red")
    .attr("stroke-width", 5)
    .attr("opacity", (d, i) => i / lastN);
}

document.addEventListener("DOMContentLoaded", async () => {
  try {
    await run();
  } catch (error) {
    // Handle errors
    console.error(error);
  }
});
