import * as Plot from "https://cdn.jsdelivr.net/npm/@observablehq/plot@0.6/+esm";

async function fetchAPI() {
  // Read the data
  console.log(localStorage.getItem(LSATK));
  let data = await d3.json(
    `${ENDPOINT}/plot-dummy-data?` +
      new URLSearchParams({
        symbol: SYMBOL,
      }).toString(),
    {
      headers: new Headers({
        Authorization: localStorage.getItem(LSATK),
      }),
    }
  );

  const options = {
    marks: [
      Plot.frame(),
      Plot.lineY(data, {
        x: "X",
        y: "Y",
        strokeWidth: 5,
        clip: true,
      }),
    ],
    grid: true,
    inset: 10,
    facet: { marginRight: 90 },
    x: {
      tickSpacing: 80,
      label: "Time",
      domain: [new Date("2024-09-12"), new Date()],
    },
    y: {
      tickSpacing: 80,
      label: "Total Karma",
      tickFormat: d3.format("~s"),
    },
    legend: false,
    title: `Karma and Posts for ${SYMBOL}`,
  };
  const plot = Plot.plot(options);
  const div = document.querySelector("#plot");
  div.append(plot);
  $("#loading").toggle();
}

$(document).ready(async () => await fetchAPI());
