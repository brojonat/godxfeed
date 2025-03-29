async function* generateData(option_ticker_symbols, interval) {
  // pick a random symbol from the list and return a random quote
  // in the following format every interval. This can be used in a
  // asynchronous stream to simulate a live feed:
  // {
  //   "eventSymbol": symbol,
  //   "bisPrice": 100,
  //   "askPrice": 100,
  //   "bidSize": 100,
  //   "askSize": 100,
  //   "eventType": "quote"
  // }
  //
  // The interval is in milliseconds.

  if (!option_ticker_symbols || option_ticker_symbols.length === 0) {
    throw new Error("No ticker symbols provided");
  }

  // Generate a random quote for a random symbol
  const generateRandomQuote = () => {
    // Pick a random symbol from the list
    const randomIndex = Math.floor(
      Math.random() * option_ticker_symbols.length
    );
    const symbol = option_ticker_symbols[randomIndex];

    // Generate random price around 100 (between 90 and 110)
    const basePrice = 90 + Math.random() * 20;
    const bidPrice = Number(basePrice.toFixed(2));
    // Ask price is slightly higher than bid price
    const askPrice = Number((bidPrice + 0.01 + Math.random() * 0.2).toFixed(2));

    // Generate random sizes between 50 and 150
    const bidSize = Math.floor(50 + Math.random() * 100);
    const askSize = Math.floor(50 + Math.random() * 100);

    return {
      eventSymbol: symbol,
      bidPrice: bidPrice,
      askPrice: askPrice,
      bidSize: bidSize,
      askSize: askSize,
      eventType: "quote",
    };
  };

  while (true) {
    // Generate a new quote
    const quote = generateRandomQuote();

    // Yield the quote and wait for the specified interval
    yield quote;

    // Wait for the interval before generating the next quote
    await new Promise((resolve) => setTimeout(resolve, interval));
  }
}

// Export the function
export { generateData };
